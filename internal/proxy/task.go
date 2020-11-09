package proxy

import (
	"context"
	"errors"
	"log"

	"github.com/zilliztech/milvus-distributed/internal/msgstream"
	"github.com/zilliztech/milvus-distributed/internal/proto/commonpb"
	"github.com/zilliztech/milvus-distributed/internal/proto/internalpb"
	"github.com/zilliztech/milvus-distributed/internal/proto/masterpb"
	"github.com/zilliztech/milvus-distributed/internal/proto/servicepb"
)

type task interface {
	Id() UniqueID // return ReqId
	Type() internalpb.MsgType
	BeginTs() Timestamp
	EndTs() Timestamp
	SetTs(ts Timestamp)
	PreExecute() error
	Execute() error
	PostExecute() error
	WaitToFinish() error
	Notify(err error)
}

type BaseInsertTask = msgstream.InsertMsg

type InsertTask struct {
	BaseInsertTask
	ts                    Timestamp
	done                  chan error
	resultChan            chan *servicepb.IntegerRangeResponse
	manipulationMsgStream *msgstream.PulsarMsgStream
	ctx                   context.Context
	cancel                context.CancelFunc
}

func (it *InsertTask) SetTs(ts Timestamp) {
	it.ts = ts
}

func (it *InsertTask) BeginTs() Timestamp {
	return it.ts
}

func (it *InsertTask) EndTs() Timestamp {
	return it.ts
}

func (it *InsertTask) Id() UniqueID {
	return it.ReqId
}

func (it *InsertTask) Type() internalpb.MsgType {
	return it.MsgType
}

func (it *InsertTask) PreExecute() error {
	return nil
}

func (it *InsertTask) Execute() error {
	var tsMsg msgstream.TsMsg = &it.BaseInsertTask
	msgPack := &msgstream.MsgPack{
		BeginTs: it.BeginTs(),
		EndTs:   it.EndTs(),
		Msgs:    make([]*msgstream.TsMsg, 1),
	}
	msgPack.Msgs[0] = &tsMsg
	it.manipulationMsgStream.Produce(msgPack)
	return nil
}

func (it *InsertTask) PostExecute() error {
	return nil
}

func (it *InsertTask) WaitToFinish() error {
	defer it.cancel()
	for {
		select {
		case err := <-it.done:
			return err
		case <-it.ctx.Done():
			log.Print("wait to finish failed, timeout!")
			return errors.New("wait to finish failed, timeout!")
		}
	}
}

func (it *InsertTask) Notify(err error) {
	it.done <- err
}

type CreateCollectionTask struct {
	internalpb.CreateCollectionRequest
	masterClient masterpb.MasterClient
	done         chan error
	resultChan   chan *commonpb.Status
	ctx          context.Context
	cancel       context.CancelFunc
}

func (cct *CreateCollectionTask) Id() UniqueID {
	return cct.ReqId
}

func (cct *CreateCollectionTask) Type() internalpb.MsgType {
	return cct.MsgType
}

func (cct *CreateCollectionTask) BeginTs() Timestamp {
	return cct.Timestamp
}

func (cct *CreateCollectionTask) EndTs() Timestamp {
	return cct.Timestamp
}

func (cct *CreateCollectionTask) SetTs(ts Timestamp) {
	cct.Timestamp = ts
}

func (cct *CreateCollectionTask) PreExecute() error {
	return nil
}

func (cct *CreateCollectionTask) Execute() error {
	resp, err := cct.masterClient.CreateCollection(cct.ctx, &cct.CreateCollectionRequest)
	if err != nil {
		log.Printf("create collection failed, error= %v", err)
		cct.resultChan <- &commonpb.Status{
			ErrorCode: commonpb.ErrorCode_UNEXPECTED_ERROR,
			Reason:    err.Error(),
		}
	} else {
		cct.resultChan <- resp
	}
	return err
}

func (cct *CreateCollectionTask) PostExecute() error {
	return nil
}

func (cct *CreateCollectionTask) WaitToFinish() error {
	defer cct.cancel()
	for {
		select {
		case err := <-cct.done:
			return err
		case <-cct.ctx.Done():
			log.Print("wait to finish failed, timeout!")
			return errors.New("wait to finish failed, timeout!")
		}
	}
}

func (cct *CreateCollectionTask) Notify(err error) {
	cct.done <- err
}

type QueryTask struct {
	internalpb.SearchRequest
	queryMsgStream *msgstream.PulsarMsgStream
	done           chan error
	resultBuf      chan []*internalpb.SearchResult
	resultChan     chan *servicepb.QueryResult
	ctx            context.Context
	cancel         context.CancelFunc
}

func (qt *QueryTask) Id() UniqueID {
	return qt.ReqId
}

func (qt *QueryTask) Type() internalpb.MsgType {
	return qt.MsgType
}

func (qt *QueryTask) BeginTs() Timestamp {
	return qt.Timestamp
}

func (qt *QueryTask) EndTs() Timestamp {
	return qt.Timestamp
}

func (qt *QueryTask) SetTs(ts Timestamp) {
	qt.Timestamp = ts
}

func (qt *QueryTask) PreExecute() error {
	return nil
}

func (qt *QueryTask) Execute() error {
	var tsMsg msgstream.TsMsg = &msgstream.SearchMsg{
		SearchRequest: qt.SearchRequest,
		BaseMsg: msgstream.BaseMsg{
			BeginTimestamp: qt.Timestamp,
			EndTimestamp:   qt.Timestamp,
		},
	}
	msgPack := &msgstream.MsgPack{
		BeginTs: qt.Timestamp,
		EndTs:   qt.Timestamp,
		Msgs:    make([]*msgstream.TsMsg, 1),
	}
	msgPack.Msgs[0] = &tsMsg
	qt.queryMsgStream.Produce(msgPack)
	return nil
}

func (qt *QueryTask) PostExecute() error {
	return nil
}

func (qt *QueryTask) WaitToFinish() error {
	defer qt.cancel()
	for {
		select {
		case err := <-qt.done:
			return err
		case <-qt.ctx.Done():
			log.Print("wait to finish failed, timeout!")
			return errors.New("wait to finish failed, timeout!")
		}
	}
}

func (qt *QueryTask) Notify(err error) {
	defer qt.cancel()
	defer func() {
		qt.done <- err
	}()
	for {
		select {
		case <-qt.ctx.Done():
			log.Print("wait to finish failed, timeout!")
			return
		case searchResults := <-qt.resultBuf:
			rlen := len(searchResults) // query num
			if rlen <= 0 {
				qt.resultChan <- &servicepb.QueryResult{}
				return
			}
			n := len(searchResults[0].Hits) // n
			if n <= 0 {
				qt.resultChan <- &servicepb.QueryResult{}
				return
			}
			k := len(searchResults[0].Hits[0].Ids) // k
			queryResult := &servicepb.QueryResult{
				Status: &commonpb.Status{
					ErrorCode: 0,
				},
			}
			// reduce by score, TODO: use better algorithm
			// use merge-sort here, the number of ways to merge is `rlen`
			// in this process, we must make sure:
			//		len(queryResult.Hits) == n
			//		len(queryResult.Hits[i].Ids) == k for i in range(n)
			for i := 0; i < n; n++ { // n
				locs := make([]int, rlen)
				hits := &servicepb.Hits{}
				for j := 0; j < k; j++ { // k
					choice, maxScore := 0, float32(0)
					for q, loc := range locs { // query num, the number of ways to merge
						score := func(score *servicepb.Score) float32 {
							// TODO: get score of root
							return 0.0
						}(searchResults[q].Hits[i].Scores[loc])
						if score > maxScore {
							choice = q
							maxScore = score
						}
					}
					choiceOffset := locs[choice]
					hits.Ids = append(hits.Ids, searchResults[choice].Hits[i].Ids[choiceOffset])
					hits.RowData = append(hits.RowData, searchResults[choice].Hits[i].RowData[choiceOffset])
					hits.Scores = append(hits.Scores, searchResults[choice].Hits[i].Scores[choiceOffset])
					locs[choice]++
				}
				queryResult.Hits = append(queryResult.Hits, hits)
			}
			qt.resultChan <- queryResult
		}
	}
}