package reader

/*

#cgo CFLAGS: -I${SRCDIR}/../core/output/include

#cgo LDFLAGS: -L${SRCDIR}/../core/output/lib -lmilvus_segcore -Wl,-rpath=${SRCDIR}/../core/output/lib

#include "collection_c.h"
#include "partition_c.h"
#include "segment_c.h"

*/
import "C"
import "github.com/zilliztech/milvus-distributed/internal/util/typeutil"

type UniqueID = typeutil.UniqueID

type Collection struct {
	CollectionPtr  C.CCollection
	CollectionName string
	CollectionID   UniqueID
	Partitions     []*Partition
}

func (c *Collection) newPartition(partitionName string) *Partition {
	/*
		CPartition
		newPartition(CCollection collection, const char* partition_name);
	*/
	cName := C.CString(partitionName)
	partitionPtr := C.NewPartition(c.CollectionPtr, cName)

	var newPartition = &Partition{PartitionPtr: partitionPtr, PartitionName: partitionName}
	c.Partitions = append(c.Partitions, newPartition)
	return newPartition
}

func (c *Collection) deletePartition(node *QueryNode, partition *Partition) {
	/*
		void
		deletePartition(CPartition partition);
	*/
	cPtr := partition.PartitionPtr
	C.DeletePartition(cPtr)

	tmpPartitions := make([]*Partition, 0)

	for _, p := range c.Partitions {
		if p.PartitionName == partition.PartitionName {
			for _, s := range p.Segments {
				delete(node.SegmentsMap, s.SegmentID)
			}
		} else {
			tmpPartitions = append(tmpPartitions, p)
		}
	}

	c.Partitions = tmpPartitions
}

func (c *Collection) getPartitionByName(partitionName string) (partition *Partition) {
	for _, partition := range c.Partitions {
		if partition.PartitionName == partitionName {
			return partition
		}
	}
	return nil
	// TODO: remove from c.Partitions
}