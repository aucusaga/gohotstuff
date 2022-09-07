package state

import (
	"fmt"

	"github.com/aucusaga/gohotstuff/libs"
	"github.com/aucusaga/gohotstuff/pb"
	"github.com/aucusaga/gohotstuff/types"
	"github.com/golang/protobuf/proto"
)

// MsgFromProto takes a consensus proto message and returns the native hotstuff types
func ConsMsgFromProto(msgbytes []byte) (MsgInfo, error) {
	var msg pb.Message
	if err := proto.Unmarshal(msgbytes, &msg); err != nil {
		return nil, fmt.Errorf("unmarshal bytes fail @ ConsMsgFromProto, err: %v", err)
	}
	if msg.Module != libs.ConsensusModule {
		return nil, fmt.Errorf("msg module invalid @ ConsMsgFromProto, want: %s, has: %s", libs.ConsensusModule, msg.Module)
	}
	var consMsg MsgInfo
	switch msg := msg.Sum.(type) {
	case *pb.Message_Proposal:
		consMsg = &types.ProposalMsg{
			Round:         msg.Proposal.Round,
			ID:            msg.Proposal.Id,
			JustifyParent: msg.Proposal.Justify,
			PeerID:        string(msg.Proposal.Pid),
			PublicKey:     msg.Proposal.Pk,
			Signature:     msg.Proposal.Signature,
			Timestamp:     msg.Proposal.Timestamp,
		}
	case *pb.Message_Vote:
		consMsg = &types.VoteMsg{
			Round:       msg.Vote.VoteInfo.ProposalRound,
			ID:          msg.Vote.VoteInfo.ProposalId,
			ParentRound: msg.Vote.VoteInfo.ParentRound,
			ParentID:    msg.Vote.VoteInfo.ParentId,
			CommitInfo:  msg.Vote.CommitInfo,
			SendID:      string(msg.Vote.Pid),
			PublicKey:   msg.Vote.Pk,
			Signature:   msg.Vote.Signature,
			Timestamp:   msg.Vote.Timestamp,
		}
	case *pb.Message_Timeout:
		consMsg = &types.TimeoutMsg{
			Round:       msg.Timeout.Round,
			Index:       msg.Timeout.Index,
			ParentRound: msg.Timeout.ParentRound,
			ParentID:    msg.Timeout.ParentId,
			SendID:      string(msg.Timeout.Pid),
			PublicKey:   msg.Timeout.Pk,
			Signature:   msg.Timeout.Signature,
			Timestamp:   msg.Timeout.Timestamp,
		}
	}

	if err := consMsg.Validate(); err != nil {
		return nil, fmt.Errorf("consMsg validation error @ ConsMsgFromProto, err: %v", err)
	}
	return consMsg, nil
}

func ProtoFromConsMsg(msg MsgInfo) ([]byte, error) {
	proto := pb.Message{
		Module: libs.ConsensusModule,
	}

	switch msg := msg.(type) {
	case *types.ProposalMsg:
		proto.Sum = &pb.Message_Proposal{
			Proposal: &pb.ProposalMessage{
				Module:    libs.ConsensusModule,
				Round:     msg.Round,
				Id:        msg.ID,
				Justify:   msg.JustifyParent,
				Timestamp: msg.Timestamp,
				Pid:       []byte(msg.PeerID),
			},
		}
	case *types.VoteMsg:
		proto.Sum = &pb.Message_Vote{
			Vote: &pb.VoteMessage{
				Module: libs.ConsensusModule,
				VoteInfo: &pb.VoteInfo{
					ProposalRound: msg.Round,
					ProposalId:    msg.ID,
					ParentRound:   msg.ParentRound,
					ParentId:      msg.ParentID,
				},
				CommitInfo: msg.CommitInfo,
				Timestamp:  msg.Timestamp,
				Pid:        []byte(msg.SendID),
			},
		}
	case *types.TimeoutMsg:
		proto.Sum = &pb.Message_Timeout{
			Timeout: &pb.TimoutMessage{
				Module:      libs.ConsensusModule,
				Round:       msg.Round,
				ParentRound: msg.ParentRound,
				ParentId:    msg.ParentID,
				Index:       msg.Index,
				Timestamp:   msg.Timestamp,
				Pid:         []byte(msg.SendID),
			},
		}
	}

	return proto.Marshal()
}

func VoteMsg(round int64, id []byte, pround int64, pid []byte, to string) *types.VoteMsg {
	return &types.VoteMsg{
		Round:       round,
		ID:          id,
		ParentRound: pround,
		ParentID:    pid,
		To:          to,
	}
}

func ProposalMsg(round int64, id []byte, justifyParent []byte) *types.ProposalMsg {
	return &types.ProposalMsg{
		Round:         round,
		ID:            id,
		JustifyParent: justifyParent,
	}
}

func TimeoutMsg(round int64, pround int64, pid []byte, idx int64) *types.TimeoutMsg {
	return &types.TimeoutMsg{
		Round:       round,
		Index:       idx,
		ParentRound: pround,
		ParentID:    pid,
	}
}
