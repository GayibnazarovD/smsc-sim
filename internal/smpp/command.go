// Package smpp implements just enough of the SMPP v3.4 wire protocol for an
// SMSC simulator: PDU framing, the bind and short-message bodies, and TLVs.
// It is deliberately small and dependency-free rather than a general-purpose
// SMPP library.
package smpp

// CommandID is an SMPP command_id value.
type CommandID uint32

// SMPP v3.4 command identifiers (section 5.1.2.1).
const (
	GenericNACK         CommandID = 0x80000000
	BindReceiver        CommandID = 0x00000001
	BindReceiverResp    CommandID = 0x80000001
	BindTransmitter     CommandID = 0x00000002
	BindTransmitterResp CommandID = 0x80000002
	QuerySM             CommandID = 0x00000003
	QuerySMResp         CommandID = 0x80000003
	SubmitSM            CommandID = 0x00000004
	SubmitSMResp        CommandID = 0x80000004
	DeliverSM           CommandID = 0x00000005
	DeliverSMResp       CommandID = 0x80000005
	Unbind              CommandID = 0x00000006
	UnbindResp          CommandID = 0x80000006
	ReplaceSM           CommandID = 0x00000007
	ReplaceSMResp       CommandID = 0x80000007
	CancelSM            CommandID = 0x00000008
	CancelSMResp        CommandID = 0x80000008
	BindTransceiver     CommandID = 0x00000009
	BindTransceiverResp CommandID = 0x80000009
	Outbind             CommandID = 0x0000000B
	EnquireLink         CommandID = 0x00000015
	EnquireLinkResp     CommandID = 0x80000015
	SubmitMulti         CommandID = 0x00000021
	SubmitMultiResp     CommandID = 0x80000021
	AlertNotification   CommandID = 0x00000102
	DataSM              CommandID = 0x00000103
	DataSMResp          CommandID = 0x80000103
)

// IsBind reports whether id is one of the three bind requests.
func (id CommandID) IsBind() bool {
	switch id {
	case BindReceiver, BindTransmitter, BindTransceiver:
		return true
	default:
		return false
	}
}

// RespID returns the matching response command_id for a request, or 0 if id is
// not a request we generate a response for.
func (id CommandID) RespID() CommandID {
	switch id {
	case BindReceiver:
		return BindReceiverResp
	case BindTransmitter:
		return BindTransmitterResp
	case BindTransceiver:
		return BindTransceiverResp
	case SubmitSM:
		return SubmitSMResp
	case DeliverSM:
		return DeliverSMResp
	case Unbind:
		return UnbindResp
	case EnquireLink:
		return EnquireLinkResp
	case DataSM:
		return DataSMResp
	default:
		return 0
	}
}

var commandNames = map[CommandID]string{
	GenericNACK: "generic_nack", BindReceiver: "bind_receiver", BindReceiverResp: "bind_receiver_resp",
	BindTransmitter: "bind_transmitter", BindTransmitterResp: "bind_transmitter_resp",
	QuerySM: "query_sm", QuerySMResp: "query_sm_resp", SubmitSM: "submit_sm", SubmitSMResp: "submit_sm_resp",
	DeliverSM: "deliver_sm", DeliverSMResp: "deliver_sm_resp", Unbind: "unbind", UnbindResp: "unbind_resp",
	ReplaceSM: "replace_sm", ReplaceSMResp: "replace_sm_resp", CancelSM: "cancel_sm", CancelSMResp: "cancel_sm_resp",
	BindTransceiver: "bind_transceiver", BindTransceiverResp: "bind_transceiver_resp", Outbind: "outbind",
	EnquireLink: "enquire_link", EnquireLinkResp: "enquire_link_resp", SubmitMulti: "submit_multi",
	SubmitMultiResp: "submit_multi_resp", AlertNotification: "alert_notification", DataSM: "data_sm", DataSMResp: "data_sm_resp",
}

// String returns the lower-case SMPP name for the command, e.g. "submit_sm".
func (id CommandID) String() string {
	if n, ok := commandNames[id]; ok {
		return n
	}
	return "unknown"
}

// Status is an SMPP command_status value.
type Status uint32

// SMPP v3.4 command_status values (section 5.1.3). Only the subset the
// simulator emits or recognises is named.
const (
	ESME_ROK          Status = 0x00000000
	ESME_RINVMSGLEN   Status = 0x00000001
	ESME_RINVCMDLEN   Status = 0x00000002
	ESME_RINVCMDID    Status = 0x00000003
	ESME_RINVBNDSTS   Status = 0x00000004
	ESME_RALYBND      Status = 0x00000005
	ESME_RSYSERR      Status = 0x00000008
	ESME_RINVSRCADR   Status = 0x0000000A
	ESME_RINVDSTADR   Status = 0x0000000B
	ESME_RINVMSGID    Status = 0x0000000C
	ESME_RBINDFAIL    Status = 0x0000000D
	ESME_RINVPASWD    Status = 0x0000000E
	ESME_RINVSYSID    Status = 0x0000000F
	ESME_RMSGQFUL     Status = 0x00000014
	ESME_RINVESMCLASS Status = 0x00000043
	ESME_RSUBMITFAIL  Status = 0x00000045
	ESME_RINVSYSTYP   Status = 0x00000053
	ESME_RTHROTTLED   Status = 0x00000058
	ESME_RX_T_APPN    Status = 0x00000064
	ESME_RX_P_APPN    Status = 0x00000065
	ESME_RX_R_APPN    Status = 0x00000066
	ESME_RUNKNOWNERR  Status = 0x000000FF
)

// InterfaceVersion values seen in bind PDUs.
const (
	Version33 uint8 = 0x33
	Version34 uint8 = 0x34
)

// TLV (optional parameter) tags used by the simulator (section 5.3.2).
const (
	TagDestAddrSubunit    uint16 = 0x0005
	TagSourceAddrSubunit  uint16 = 0x000D
	TagUserMessageRef     uint16 = 0x0204
	TagSARMsgRefNum       uint16 = 0x020C
	TagSARTotalSegments   uint16 = 0x020E
	TagSARSegmentSeqnum   uint16 = 0x020F
	TagSCInterfaceVersion uint16 = 0x0210
	TagReceiptedMessageID uint16 = 0x001E
	TagMessageStateOption uint16 = 0x0427
	TagNetworkErrorCode   uint16 = 0x0423
	TagMessagePayload     uint16 = 0x0424
	TagMoreMessagesToSend uint16 = 0x0426
)

// MessageState values for the message_state TLV (section 5.2.28).
const (
	StateEnroute       uint8 = 1
	StateDelivered     uint8 = 2
	StateExpired       uint8 = 3
	StateDeleted       uint8 = 4
	StateUndeliverable uint8 = 5
	StateAccepted      uint8 = 6
	StateUnknown       uint8 = 7
	StateRejected      uint8 = 8
)

// ESMClass bit for a delivery-receipt deliver_sm (section 5.2.12).
const ESMClassDeliveryReceipt uint8 = 0x04

// Data coding schemes the simulator recognises for length accounting.
const (
	DataCodingDefault uint8 = 0x00
	DataCodingLatin1  uint8 = 0x03
	DataCodingBinary  uint8 = 0x04
	DataCodingUCS2    uint8 = 0x08
)
