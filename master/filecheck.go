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
	"fmt"
	"sort"
	"strconv"
	"time"
)

const (
	tinyExtentCount   = 128
	tinyExtentStartID = 5000000
)

// recover a file if it has bad CRC or it has been timed out before
func (partition *DataPartition) checkFile(clusterID string) {
	partition.Lock()
	defer partition.Unlock()
	liveReplicas := partition.getLiveReplicas(defaultDataPartitionTimeOutSec)
	if len(liveReplicas) == 0 {
		return
	}

	if len(liveReplicas) < int(partition.ReplicaNum) {
		liveAddrs := make([]string, 0)
		for _, replica := range liveReplicas {
			liveAddrs = append(liveAddrs, replica.Addr)
		}
		unliveAddrs := make([]string, 0)
		for _, host := range partition.PersistenceHosts {
			if !contains(liveAddrs, host) {
				unliveAddrs = append(unliveAddrs, host)
			}
		}
		Warn(clusterID, fmt.Sprintf("vol[%v],dpId[%v],liveAddrs[%v],unliveAddrs[%v]", partition.VolName, partition.PartitionID, liveAddrs, unliveAddrs))
	}
	partition.checkFileInternal(liveReplicas, clusterID)
	return
}

func (partition *DataPartition) checkFileInternal(liveReplicas []*DataReplica, clusterID string) {
	for _, fc := range partition.FileInCoreMap {
		extentID, err := strconv.ParseUint(fc.Name, 10, 64)
		if err != nil {
			continue
		}
		if isTinyExtent(extentID) {
			partition.checkChunkFile(fc, liveReplicas, clusterID)
		} else {
			partition.checkExtentFile(fc, liveReplicas, clusterID)
		}
	}
}
func isTinyExtent(extentID uint64) bool {
	return extentID >= tinyExtentStartID && extentID < tinyExtentStartID+tinyExtentCount
}

func (partition *DataPartition) checkChunkFile(fc *FileInCore, liveReplicas []*DataReplica, clusterID string) {
	if fc.isCheckCrc() == false {
		return
	}
	fms, _ := fc.needCrcRepair(liveReplicas)

	if isSameSize(fms) {
		return
	}
	msg := fmt.Sprintf("CheckFileError size not match,cluster[%v],", clusterID)
	for _, fm := range fms {
		msg = fmt.Sprintf(msg+"fm[%v]:%v\n", fm.locIndex, fm)
	}
	Warn(clusterID, msg)
	return
}

func (partition *DataPartition) checkExtentFile(fc *FileInCore, liveReplicas []*DataReplica, clusterID string) {
	if fc.isCheckCrc() == false {
		return
	}

	fms, needRepair := fc.needCrcRepair(liveReplicas)

	if len(fms) < len(liveReplicas) && (time.Now().Unix()-fc.LastModify) > checkMissFileReplicaTime {
		liveAddrs := make([]string, 0)
		for _, replica := range liveReplicas {
			liveAddrs = append(liveAddrs, replica.Addr)
		}
		Warn(clusterID, fmt.Sprintf("partitionid[%v],file[%v],fms[%v],liveAddr[%v]", partition.PartitionID, fc.Name, fc.getFileMetaAddrs(), liveAddrs))
	}
	if !needRepair {
		return
	}

	fileCrcArr := fc.calculateCrcCount(fms)
	sort.Sort((fileCrcSorterByCount)(fileCrcArr))
	maxCountFileCrcIndex := len(fileCrcArr) - 1
	if fileCrcArr[maxCountFileCrcIndex].count == 1 {
		msg := fmt.Sprintf("checkFileCrcTaskErr clusterID[%v] partitionID:%v  File:%v  ExtentOffset diffrent between all Node  "+
			" it can not repair it ", clusterID, partition.PartitionID, fc.Name)
		msg += (fileCrcSorterByCount)(fileCrcArr).log()
		Warn(clusterID, msg)
		return
	}

	for index, crc := range fileCrcArr {
		if index != maxCountFileCrcIndex {
			badNode := crc.meta
			msg := fmt.Sprintf("checkFileCrcTaskErr clusterID[%v] partitionID:%v  File:%v  badCrc On :%v  ",
				clusterID, partition.PartitionID, fc.Name, badNode.getLocationAddr())
			msg += (fileCrcSorterByCount)(fileCrcArr).log()
			Warn(clusterID, msg)
		}
	}
	return
}
