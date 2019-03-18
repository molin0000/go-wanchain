package core

import (
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/wanchain/go-wanchain/core/types"
	"github.com/wanchain/go-wanchain/pos/posdb"
	"crypto/ecdsa"
	"encoding/json"
	"github.com/wanchain/go-wanchain/core/vm"
	"github.com/wanchain/go-wanchain/crypto"
	"github.com/wanchain/go-wanchain/log"
	"github.com/wanchain/go-wanchain/pos/postools"
)

type RbLeadersSelInt interface {
	GetTargetBlkNumber(epochId uint64) uint64
	GetRBProposerGroup(epochID uint64) []vm.Leader
}

type SlLeadersSelInt interface {
	GetEpochLeadersPK(epochID uint64) []*ecdsa.PublicKey
}

const (
	BLOCKCHAIN  = iota //0
	HEADERCHAIN        //1
)

type ForkMemBlockChain struct {
	useEpochGenesis    bool
	rbLeaderSelector   RbLeadersSelInt
	slotLeaderSelector SlLeadersSelInt

	epochGens    	   map[uint64]*types.EpochGenesis

}

func NewForkMemBlockChain() *ForkMemBlockChain {

	f := &ForkMemBlockChain{}
	f.useEpochGenesis = false
	f.epochGens = make(map[uint64]*types.EpochGenesis)

	return f
}

type BlockSorter []*types.Block

//Len()
func (s BlockSorter) Len() int {
	return len(s)
}

func (s BlockSorter) Less(i, j int) bool {
	return s[i].NumberU64() < s[j].NumberU64()
}

//Swap()
func (s BlockSorter) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}


func (f *ForkMemBlockChain) GetBlockEpochIdAndSlotId(header *types.Header) (blkEpochId uint64, blkSlotId uint64, err error) {
	blkTime := header.Time.Uint64()

	blkTd := header.Difficulty.Uint64()

	blkEpochId = (blkTd >> 32)
	blkSlotId = ((blkTd & 0xffffffff) >> 8)

	calEpochId, calSlotId := postools.CalEpochSlotID(blkTime)
	//calEpochId,calSlotId := uint64(blkTime),uint64(blkTime)

	if calEpochId != blkEpochId {
		fmt.Println(calEpochId, blkEpochId, calSlotId, blkSlotId)
		return 0, 0, errors.New("epochid and slotid is not match with blk time")
	}

	return
}


func (f *ForkMemBlockChain) updateReOrg(epochId uint64, length uint64) {
	reOrgDb := posdb.GetDbByName("forkdb")
	if reOrgDb == nil {
		reOrgDb = posdb.NewDb("forkdb")
	}

	numberBytes, _ := reOrgDb.Get(epochId, "reorgNumber")

	num := uint64(0)
	if numberBytes != nil {
		num = binary.BigEndian.Uint64(numberBytes) + 1
	}

	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, num)

	reOrgDb.Put(epochId, "reorgNumber", b)

	b = make([]byte, 8)
	binary.BigEndian.PutUint64(b, length)
	reOrgDb.Put(epochId, "reorgLength", b)
}

func (f *ForkMemBlockChain) updateFork(epochId uint64) {
	reOrgDb := posdb.GetDbByName("forkdb")
	if reOrgDb == nil {
		reOrgDb = posdb.NewDb("forkdb")
	}

	numberBytes, _ := reOrgDb.Get(0, "forkNumber")

	num := uint64(0)
	if numberBytes != nil {

		num = binary.BigEndian.Uint64(numberBytes) + 1
	}

	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, num)

	reOrgDb.Put(epochId, "forkNumber", b)
}


func (f *ForkMemBlockChain) GetEpochGenesis(epochid uint64,blk *types.Block) (*types.EpochGenesis,error){


	if blk==nil {
		return nil, errors.New("blk is nil")
	}
	epGen := &types.EpochGenesis{}
	epGen.ProtocolMagic = []byte("wanchainpos")
	epGen.EpochId = epochid
	epGen.PreEpochLastBlkHash = blk.Hash()

	epGen.RBLeaders = make([][]byte,0)
	rbleaders := f.rbLeaderSelector.GetRBProposerGroup(epochid)
	if len(rbleaders)!=0 {
		for _, rbl := range rbleaders {
			epGen.RBLeaders = append(epGen.RBLeaders, rbl.PubSec256)
		}
	}

	epGen.SlotLeaders = make([][]byte,0)
	pks := f.slotLeaderSelector.GetEpochLeadersPK(epochid)
	if len(pks)!=0 {
		for _, slpk := range pks {
			epGen.SlotLeaders = append(epGen.SlotLeaders, crypto.FromECDSAPub(slpk))
		}
	}

	byteVal, err := json.Marshal(epGen)

	if err != nil {
		return nil, err
	}

	epGen.GenesisBlkHash = crypto.Keccak256Hash(byteVal)

	return epGen,nil
}

func (f *ForkMemBlockChain) VerifyEpochGenesis(bc *BlockChain, blk *types.Block) bool {

	if !f.useEpochGenesis {
		return true
	}

	epGen := &types.EpochGenesis{}
	epGen.ProtocolMagic = []byte("wanchainpos")

	epochid, _, err := f.GetBlockEpochIdAndSlotId(blk.Header())
	if err != nil {
		log.Info("verify genesis failed because of wrong epochid or slotid")
		return false
	}
	epGen.EpochId = epochid

	lastBlk := bc.GetBlockByNumber(blk.NumberU64() - 1)
	epGen.PreEpochLastBlkHash = lastBlk.Hash()

	epGen.RBLeaders = make([][]byte, 0)
	leaders := f.rbLeaderSelector.GetRBProposerGroup(epochid)
	for _, ldr := range leaders {
		epGen.RBLeaders = append(epGen.RBLeaders, ldr.PubSec256)
	}

	epGen.SlotLeaders = make([][]byte, 0)
	pks := f.slotLeaderSelector.GetEpochLeadersPK(epochid)
	if pks != nil {
		for _, slpk := range pks {
			epGen.SlotLeaders = append(epGen.SlotLeaders, crypto.FromECDSAPub(slpk))
		}
	}

	byteVal, err := json.Marshal(epGen)

	if err != nil {
		log.Info("verify genesis marshal failed")
		return false
	}

	genesisBlkHash := crypto.Keccak256Hash(byteVal)

	res := (genesisBlkHash == blk.ParentHash())

	return res
}

func (f *ForkMemBlockChain) IsFirstBlockInEpoch(firstBlk *types.Block) bool {
	_, slotid, err := f.GetBlockEpochIdAndSlotId(firstBlk.Header())
	if err != nil {
		log.Info("verify genesis failed because of wrong epochid or slotid")
		return false
	}

	if slotid == 0 {
		return true
	}

	return false
}

func (f *ForkMemBlockChain) GetLastBlkInPreEpoch(bc *BlockChain, firstBlk *types.Block) *types.Block {
	return nil
}

func (f *ForkMemBlockChain) IsExistEpochGenesis(epochid uint64) bool {
	return f.epochGens[epochid] != nil
}

func (f *ForkMemBlockChain) SetEpochGenesis(epochgen *types.EpochGenesis) error{
	f.epochGens[epochgen.EpochId] = epochgen
	return nil
}
