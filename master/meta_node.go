// Copyright 2018 The CFS Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
// implied. See the License for the specific language governing
// permissions and limitations under the License.

package master

import (
	"encoding/json"
	"github.com/tiglabs/containerfs/proto"
	"math/rand"
	"sync"
	"time"
)

// MetaNode defines the structure of a meta node
type MetaNode struct {
	ID                 uint64
	Addr               string
	IsActive           bool
	Sender             *AdminTaskSender
	RackName           string `json:"Rack"`
	MaxMemAvailWeight  uint64 `json:"MaxMemAvailWeight"`
	Total              uint64 `json:"TotalWeight"`
	Used               uint64 `json:"UsedWeight"`
	Ratio              float64
	SelectCount        uint64
	Carry              float64
	Threshold          float32
	ReportTime         time.Time
	metaPartitionInfos []*proto.MetaPartitionReport
	MetaPartitionCount int
	NodeSetID          uint64
	sync.RWMutex
}

func newMetaNode(addr, clusterID string) (node *MetaNode) {
	return &MetaNode{
		Addr:   addr,
		Sender: newAdminTaskSender(addr, clusterID),
		Carry:  rand.Float64(),
	}
}

func (metaNode *MetaNode) clean() {
	metaNode.Sender.exitCh <- struct{}{}
}

// SetCarry implements the Node interface
func (metaNode *MetaNode) SetCarry(carry float64) {
	metaNode.Lock()
	defer metaNode.Unlock()
	metaNode.Carry = carry
}

// SelectNodeForWrite implements the Node interface
func (metaNode *MetaNode) SelectNodeForWrite() {
	metaNode.Lock()
	defer metaNode.Unlock()
	metaNode.SelectCount++
	metaNode.Carry = metaNode.Carry - 1.0
}

func (metaNode *MetaNode) isWriteAble() (ok bool) {
	metaNode.RLock()
	defer metaNode.RUnlock()
	if metaNode.IsActive && metaNode.MaxMemAvailWeight > defaultMetaNodeReservedMem &&
		!metaNode.reachesThreshold() && metaNode.MetaPartitionCount < defaultMetaPartitionCountOnEachNode {
		ok = true
	}
	return
}

// TODO what is a carry node?
func (metaNode *MetaNode) isAvailCarryNode() (ok bool) {
	metaNode.RLock()
	defer metaNode.RUnlock()
	return metaNode.Carry >= 1
}

// TODO setNodeActive?
func (metaNode *MetaNode) setNodeAlive() {
	metaNode.Lock()
	defer metaNode.Unlock()
	metaNode.ReportTime = time.Now()
	metaNode.IsActive = true
}

func (metaNode *MetaNode) updateMetric(resp *proto.MetaNodeHeartbeatResponse, threshold float32) {
	metaNode.Lock()
	defer metaNode.Unlock()
	metaNode.metaPartitionInfos = resp.MetaPartitionInfo
	metaNode.MetaPartitionCount = len(metaNode.metaPartitionInfos)
	metaNode.Total = resp.Total
	metaNode.Used = resp.Used
	if resp.Total == 0 {
		metaNode.Ratio = 0
	} else {
		metaNode.Ratio = float64(resp.Used) / float64(resp.Total)
	}
	metaNode.MaxMemAvailWeight = resp.Total - resp.Used
	metaNode.RackName = resp.RackName
	metaNode.Threshold = threshold
}

func (metaNode *MetaNode) reachesThreshold() bool {
	if metaNode.Threshold <= 0 {
		metaNode.Threshold = defaultMetaPartitionThreshold
	}
	return float32(float64(metaNode.Used)/float64(metaNode.Total)) > metaNode.Threshold
}

// TODO newHeartbeatTask
func (metaNode *MetaNode) generateHeartbeatTask(masterAddr string) (task *proto.AdminTask) {
	request := &proto.HeartBeatRequest{
		CurrTime:   time.Now().Unix(),
		MasterAddr: masterAddr,
	}
	task = proto.NewAdminTask(proto.OpMetaNodeHeartbeat, metaNode.Addr, request)
	return
}

func (metaNode *MetaNode) checkHeartbeat() {
	metaNode.Lock()
	defer metaNode.Unlock()
	if time.Since(metaNode.ReportTime) > time.Second*time.Duration(defaultNodeTimeOutSec) {
		metaNode.IsActive = false
	}
}

func (metaNode *MetaNode) toJSON() (body []byte, err error) {
	metaNode.RLock()
	defer metaNode.RUnlock()
	return json.Marshal(metaNode)
}
