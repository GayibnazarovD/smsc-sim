// Package smpp implements the SMPP wire protocol for an SMSC simulator:
// PDU framing, bind and short-message bodies, optional TLVs, and status codes.
// Supports SMPP v3.3, v3.4, and v5.0 specifications (https://smpp.org/).
package smpp

import (
	"fmt"
	"strconv"
	"strings"
)

// CommandID is an SMPP command_id value.
type CommandID uint32

// SMPP command identifiers (section 5.1.2.1).
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

// Full SMPP command_status error codes (from smpp.org v3.3, v3.4, and v5.0).
const (
	ESME_ROK              Status = 0x00000000 // No Error (Success)
	ESME_RINVMSGLEN       Status = 0x00000001 // Message Length is invalid
	ESME_RINVCMDLEN       Status = 0x00000002 // Command Length is invalid
	ESME_RINVCMDID        Status = 0x00000003 // Invalid Command ID
	ESME_RINVBNDSTS       Status = 0x00000004 // Incorrect BIND Status for given command
	ESME_RALYBND          Status = 0x00000005 // ESME Already in Bound State
	ESME_RINVPRTFLG       Status = 0x00000006 // Invalid Priority Flag
	ESME_RINVREGDLVFLG    Status = 0x00000007 // Invalid Registered Delivery Flag
	ESME_RSYSERR          Status = 0x00000008 // System Error
	ESME_RINVSRCADR       Status = 0x0000000A // Invalid Source Address
	ESME_RINVDSTADR       Status = 0x0000000B // Invalid Destination Address
	ESME_RINVMSGID        Status = 0x0000000C // Message ID is invalid
	ESME_RBINDFAIL        Status = 0x0000000D // Bind Failed
	ESME_RINVPASWD        Status = 0x0000000E // Invalid Password
	ESME_RINVSYSID        Status = 0x0000000F // Invalid System ID
	ESME_RCANCELFAIL      Status = 0x00000011 // Cancel SM Failed
	ESME_RREPLACEFAIL     Status = 0x00000013 // Replace SM Failed
	ESME_RMSGQFUL         Status = 0x00000014 // Message Queue Full
	ESME_RINVSERTYP       Status = 0x00000015 // Invalid Service Type
	ESME_RINVNUMDESTS     Status = 0x00000033 // Invalid number of destinations
	ESME_RINVDLNAME       Status = 0x00000034 // Invalid Distribution List name
	ESME_RINVDESTFLAG     Status = 0x00000040 // Destination flag is invalid
	ESME_RINVSUBREP       Status = 0x00000042 // Invalid submit with replace request
	ESME_RINVESMCLASS     Status = 0x00000043 // Invalid esm_class field data
	ESME_RCNTSUBDL        Status = 0x00000044 // Cannot Submit to Distribution List
	ESME_RSUBMITFAIL      Status = 0x00000045 // Submit message failed
	ESME_RINVSRCTON       Status = 0x00000048 // Invalid Source address TON
	ESME_RINVSRCNPI       Status = 0x00000049 // Invalid Source address NPI
	ESME_RINVDSTTON       Status = 0x00000050 // Invalid Destination address TON
	ESME_RINVDSTNPI       Status = 0x00000051 // Invalid Destination address NPI
	ESME_RINVSYSTYP       Status = 0x00000053 // Invalid system_type field
	ESME_RTHROTTLED       Status = 0x00000058 // Throttling Error (Rate limit exceeded)
	ESME_RCONGESTION      Status = 0x00000059 // Congestion Error (SMPP v5.0)
	ESME_RINVSCHED        Status = 0x00000061 // Invalid Scheduled Delivery Time
	ESME_RINVEXPIRY       Status = 0x00000062 // Invalid message validity period
	ESME_RINVDFTMSGID     Status = 0x00000063 // Predefined Message Invalid or not found
	ESME_RX_T_APPN        Status = 0x00000064 // ESME Receiver Temporary App Error Code
	ESME_RX_P_APPN        Status = 0x00000065 // ESME Receiver Permanent App Error Code
	ESME_RX_R_APPN        Status = 0x00000066 // ESME Receiver Reject Message Error Code
	ESME_RQUERYFAIL       Status = 0x00000067 // query_sm request failed
	ESME_RINVOPTPARSTREAM Status = 0x000000C0 // Error in the optional part of the PDU Body
	ESME_ROPTPARNOTALLWD  Status = 0x000000C1 // Optional Parameter not allowed
	ESME_RINVPARLEN       Status = 0x000000C2 // Invalid Parameter Length
	ESME_RMISSINGOPTPARAM Status = 0x000000C3 // Expected Optional Parameter missing
	ESME_RINVOPTPARAMVAL  Status = 0x000000C4 // Invalid Optional Parameter Value
	ESME_RDELIVERYFAILURE Status = 0x000000FE // Delivery Failure (used for data_sm_resp)
	ESME_RUNKNOWNERR      Status = 0x000000FF // Unknown Error
	ESME_RSERTYPUNAUTH    Status = 0x00000100 // Service Type Not Authorized (SMPP v5.0)
	ESME_RPROHIBITED      Status = 0x00000101 // Prohibited Destination (SMPP v5.0)
	ESME_RSERTYPUNAVAIL   Status = 0x00000102 // Service Type Unavailable (SMPP v5.0)
)

var statusDetails = []struct {
	Status      Status
	Name        string
	Description string
}{
	{ESME_ROK, "ESME_ROK", "No Error (Success)"},
	{ESME_RINVMSGLEN, "ESME_RINVMSGLEN", "Message Length is invalid"},
	{ESME_RINVCMDLEN, "ESME_RINVCMDLEN", "Command Length is invalid"},
	{ESME_RINVCMDID, "ESME_RINVCMDID", "Invalid Command ID"},
	{ESME_RINVBNDSTS, "ESME_RINVBNDSTS", "Incorrect BIND Status for given command"},
	{ESME_RALYBND, "ESME_RALYBND", "ESME Already in Bound State"},
	{ESME_RINVPRTFLG, "ESME_RINVPRTFLG", "Invalid Priority Flag"},
	{ESME_RINVREGDLVFLG, "ESME_RINVREGDLVFLG", "Invalid Registered Delivery Flag"},
	{ESME_RSYSERR, "ESME_RSYSERR", "System Error"},
	{ESME_RINVSRCADR, "ESME_RINVSRCADR", "Invalid Source Address"},
	{ESME_RINVDSTADR, "ESME_RINVDSTADR", "Invalid Destination Address"},
	{ESME_RINVMSGID, "ESME_RINVMSGID", "Message ID is invalid"},
	{ESME_RBINDFAIL, "ESME_RBINDFAIL", "Bind Failed"},
	{ESME_RINVPASWD, "ESME_RINVPASWD", "Invalid Password"},
	{ESME_RINVSYSID, "ESME_RINVSYSID", "Invalid System ID"},
	{ESME_RCANCELFAIL, "ESME_RCANCELFAIL", "Cancel SM Failed"},
	{ESME_RREPLACEFAIL, "ESME_RREPLACEFAIL", "Replace SM Failed"},
	{ESME_RMSGQFUL, "ESME_RMSGQFUL", "Message Queue Full"},
	{ESME_RINVSERTYP, "ESME_RINVSERTYP", "Invalid Service Type"},
	{ESME_RINVNUMDESTS, "ESME_RINVNUMDESTS", "Invalid number of destinations"},
	{ESME_RINVDLNAME, "ESME_RINVDLNAME", "Invalid Distribution List name"},
	{ESME_RINVDESTFLAG, "ESME_RINVDESTFLAG", "Destination flag is invalid"},
	{ESME_RINVSUBREP, "ESME_RINVSUBREP", "Invalid submit with replace request"},
	{ESME_RINVESMCLASS, "ESME_RINVESMCLASS", "Invalid esm_class field data"},
	{ESME_RCNTSUBDL, "ESME_RCNTSUBDL", "Cannot Submit to Distribution List"},
	{ESME_RSUBMITFAIL, "ESME_RSUBMITFAIL", "Submit message failed"},
	{ESME_RINVSRCTON, "ESME_RINVSRCTON", "Invalid Source address TON"},
	{ESME_RINVSRCNPI, "ESME_RINVSRCNPI", "Invalid Source address NPI"},
	{ESME_RINVDSTTON, "ESME_RINVDSTTON", "Invalid Destination address TON"},
	{ESME_RINVDSTNPI, "ESME_RINVDSTNPI", "Invalid Destination address NPI"},
	{ESME_RINVSYSTYP, "ESME_RINVSYSTYP", "Invalid system_type field"},
	{ESME_RTHROTTLED, "ESME_RTHROTTLED", "Throttling Error (Rate limit exceeded)"},
	{ESME_RCONGESTION, "ESME_RCONGESTION", "Congestion Error (SMPP v5.0)"},
	{ESME_RINVSCHED, "ESME_RINVSCHED", "Invalid Scheduled Delivery Time"},
	{ESME_RINVEXPIRY, "ESME_RINVEXPIRY", "Invalid message validity period"},
	{ESME_RINVDFTMSGID, "ESME_RINVDFTMSGID", "Predefined Message Invalid or not found"},
	{ESME_RX_T_APPN, "ESME_RX_T_APPN", "ESME Receiver Temporary App Error Code"},
	{ESME_RX_P_APPN, "ESME_RX_P_APPN", "ESME Receiver Permanent App Error Code"},
	{ESME_RX_R_APPN, "ESME_RX_R_APPN", "ESME Receiver Reject Message Error Code"},
	{ESME_RQUERYFAIL, "ESME_RQUERYFAIL", "query_sm request failed"},
	{ESME_RINVOPTPARSTREAM, "ESME_RINVOPTPARSTREAM", "Error in optional part of PDU Body"},
	{ESME_ROPTPARNOTALLWD, "ESME_ROPTPARNOTALLWD", "Optional Parameter not allowed"},
	{ESME_RINVPARLEN, "ESME_RINVPARLEN", "Invalid Parameter Length"},
	{ESME_RMISSINGOPTPARAM, "ESME_RMISSINGOPTPARAM", "Expected Optional Parameter missing"},
	{ESME_RINVOPTPARAMVAL, "ESME_RINVOPTPARAMVAL", "Invalid Optional Parameter Value"},
	{ESME_RDELIVERYFAILURE, "ESME_RDELIVERYFAILURE", "Delivery Failure (used for data_sm_resp)"},
	{ESME_RUNKNOWNERR, "ESME_RUNKNOWNERR", "Unknown Error"},
	{ESME_RSERTYPUNAUTH, "ESME_RSERTYPUNAUTH", "Service Type Not Authorized (SMPP v5.0)"},
	{ESME_RPROHIBITED, "ESME_RPROHIBITED", "Prohibited Destination (SMPP v5.0)"},
	{ESME_RSERTYPUNAVAIL, "ESME_RSERTYPUNAVAIL", "Service Type Unavailable (SMPP v5.0)"},
}

// String returns the canonical SMPP constant name, e.g. "ESME_RTHROTTLED".
func (s Status) String() string {
	for _, item := range statusDetails {
		if item.Status == s {
			return item.Name
		}
	}
	return fmt.Sprintf("ESME_STATUS_0x%08X", uint32(s))
}

// StatusDetail describes an SMPP status code with its hex code, name, and description.
type StatusDetail struct {
	Code        uint32 `json:"code"`
	Hex         string `json:"hex"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// AllStatuses returns the full catalogue of SMPP status codes for UI and testing.
func AllStatuses() []StatusDetail {
	res := make([]StatusDetail, len(statusDetails))
	for i, item := range statusDetails {
		res[i] = StatusDetail{
			Code:        uint32(item.Status),
			Hex:         fmt.Sprintf("0x%08X", uint32(item.Status)),
			Name:        item.Name,
			Description: item.Description,
		}
	}
	return res
}

// ParseStatus parses an SMPP status from hex ("0x58", "0x00000058"), decimal ("88"),
// or canonical constant name ("ESME_RTHROTTLED", "RTHROTTLED", "THROTTLED").
func ParseStatus(s string) (Status, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return ESME_ROK, false
	}
	// Try hex
	if strings.HasPrefix(strings.ToLower(s), "0x") {
		v, err := strconv.ParseUint(s[2:], 16, 32)
		if err == nil {
			return Status(v), true
		}
	}
	// Try decimal
	if v, err := strconv.ParseUint(s, 10, 32); err == nil {
		return Status(v), true
	}
	// Try matching by name
	clean := strings.ToUpper(s)
	if !strings.HasPrefix(clean, "ESME_") {
		clean = "ESME_" + clean
	}
	for _, item := range statusDetails {
		if item.Name == clean || strings.TrimPrefix(item.Name, "ESME_") == strings.TrimPrefix(clean, "ESME_") {
			return item.Status, true
		}
	}
	return ESME_RUNKNOWNERR, false
}

// InterfaceVersion values seen in bind PDUs and advertised in bind_resp.
const (
	Version33 uint8 = 0x33 // SMPP v3.3
	Version34 uint8 = 0x34 // SMPP v3.4
	Version50 uint8 = 0x50 // SMPP v5.0
)

// TLV (optional parameter) tags used by the simulator (section 5.3.2 & SMPP v5.0).
const (
	TagDestAddrSubunit    uint16 = 0x0005
	TagSourceAddrSubunit  uint16 = 0x000D
	TagReceiptedMessageID uint16 = 0x001E
	TagUserMessageRef     uint16 = 0x0204
	TagSARMsgRefNum       uint16 = 0x020C
	TagSARTotalSegments   uint16 = 0x020E
	TagSARSegmentSeqnum   uint16 = 0x020F
	TagSCInterfaceVersion uint16 = 0x0210
	TagNetworkErrorCode   uint16 = 0x0423
	TagMessagePayload     uint16 = 0x0424
	TagMoreMessagesToSend uint16 = 0x0426
	TagMessageStateOption uint16 = 0x0427
	TagCongestionState    uint16 = 0x0428 // SMPP v5.0 congestion state TLV (0-100)
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
