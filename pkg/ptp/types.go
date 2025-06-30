package ptp

import (
	"encoding/binary"
	"fmt"
)

// PTP Protocol Constants
const (
	PTPMajorVersion = 2
	PTPMinorVersion = 0
	PTPVersion      = (PTPMinorVersion << 4) | PTPMajorVersion
)

// Message Types
const (
	SYNC                  = 0x0
	DELAY_REQ             = 0x1
	PDELAY_REQ            = 0x2
	PDELAY_RESP           = 0x3
	FOLLOW_UP             = 0x8
	DELAY_RESP            = 0x9
	PDELAY_RESP_FOLLOW_UP = 0xA
	ANNOUNCE              = 0xB
	SIGNALING             = 0xC
	MANAGEMENT            = 0xD
)

// Management Actions
const (
	GET         = 0
	SET         = 1
	RESPONSE    = 2
	COMMAND     = 3
	ACKNOWLEDGE = 4
)

// Management IDs
const (
	MID_NULL_MANAGEMENT                    = 0x0000
	MID_CLOCK_DESCRIPTION                  = 0x0001
	MID_DEFAULT_DATA_SET                   = 0x2000
	MID_CURRENT_DATA_SET                   = 0x2001
	MID_PARENT_DATA_SET                    = 0x2002
	MID_TIME_PROPERTIES_DATA_SET           = 0x2003
	MID_PORT_DATA_SET                      = 0x2004
	MID_PRIORITY1                          = 0x2005
	MID_PRIORITY2                          = 0x2006
	MID_DOMAIN                             = 0x2007
	MID_SLAVE_ONLY                         = 0x2008
	MID_LOG_ANNOUNCE_INTERVAL              = 0x2009
	MID_ANNOUNCE_RECEIPT_TIMEOUT           = 0x200A
	MID_LOG_SYNC_INTERVAL                  = 0x200B
	MID_VERSION_NUMBER                     = 0x200C
	MID_ENABLE_PORT                        = 0x200D
	MID_DISABLE_PORT                       = 0x200E
	MID_TIME                               = 0x200F
	MID_CLOCK_ACCURACY                     = 0x2010
	MID_UTC_PROPERTIES                     = 0x2011
	MID_TRACEABILITY_PROPERTIES            = 0x2012
	MID_TIMESCALE_PROPERTIES               = 0x2013
	MID_UNICAST_NEGOTIATION_ENABLE         = 0x2014
	MID_PATH_TRACE_LIST                    = 0x2015
	MID_PATH_TRACE_ENABLE                  = 0x2016
	MID_GRANDMASTER_CLUSTER_TABLE          = 0x2017
	MID_UNICAST_MASTER_TABLE               = 0x2018
	MID_UNICAST_MASTER_MAX_TABLE_SIZE      = 0x2019
	MID_ACCEPTABLE_MASTER_TABLE            = 0x201A
	MID_ACCEPTABLE_MASTER_TABLE_ENABLED    = 0x201B
	MID_ACCEPTABLE_MASTER_MAX_TABLE_SIZE   = 0x201C
	MID_ALTERNATE_MASTER                   = 0x201D
	MID_ALTERNATE_TIME_OFFSET_ENABLE       = 0x201E
	MID_ALTERNATE_TIME_OFFSET_NAME         = 0x201F
	MID_ALTERNATE_TIME_OFFSET_MAX_KEY      = 0x2020
	MID_ALTERNATE_TIME_OFFSET_PROPERTIES   = 0x2021
	MID_TRANSPARENT_CLOCK_DEFAULT_DATA_SET = 0x4000
	MID_TRANSPARENT_CLOCK_PORT_DATA_SET    = 0x4001
	MID_PRIMARY_DOMAIN                     = 0x4002
	MID_DELAY_MECHANISM                    = 0x6000
	MID_LOG_MIN_PDELAY_REQ_INTERVAL        = 0x6001

	// Non-standard (implementation specific) management IDs
	MID_TIME_STATUS_NP                     = 0xC000
	MID_GRANDMASTER_SETTINGS_NP            = 0xC001
	MID_CLOCK_DESCRIPTION_NP               = 0xC002
	MID_PORT_DATA_SET_NP                   = 0xC002
	MID_SUBSCRIBE_EVENTS_NP                = 0xC003
	MID_PORT_PROPERTIES_NP                 = 0xC004
	MID_PORT_STATS_NP                      = 0xC005
	MID_SYNCHRONIZATION_UNCERTAIN_NP       = 0xC006
	MID_PORT_SERVICE_STATS_NP              = 0xC007
	MID_UNICAST_MASTER_TABLE_NP            = 0xC009
	MID_PORT_HWCLOCK_NP                    = 0xC00A
	MID_POWER_PROFILE_SETTINGS_NP          = 0xC00B
	MID_CMLDS_INFO_NP                      = 0xC00C
	MID_EXTERNAL_GRANDMASTER_PROPERTIES_NP = 0xC00D
)

// Port States
const (
	PS_INITIALIZING = 1 + iota
	PS_FAULTY
	PS_DISABLED
	PS_LISTENING
	PS_PRE_MASTER
	PS_MASTER
	PS_PASSIVE
	PS_UNCALIBRATED
	PS_SLAVE
)

// TLV Types
const (
	TLV_MANAGEMENT                              = 0x0001
	TLV_MANAGEMENT_ERROR_STATUS                 = 0x0002
	TLV_ORGANIZATION_EXTENSION                  = 0x0003
	TLV_REQUEST_UNICAST_TRANSMISSION            = 0x0004
	TLV_GRANT_UNICAST_TRANSMISSION              = 0x0005
	TLV_CANCEL_UNICAST_TRANSMISSION             = 0x0006
	TLV_ACKNOWLEDGE_CANCEL_UNICAST_TRANSMISSION = 0x0007
	TLV_PATH_TRACE                              = 0x0008
	TLV_ALTERNATE_TIME_OFFSET_INDICATOR         = 0x0009
	TLV_AUTHENTICATION                          = 0x2000
	TLV_AUTHENTICATION_CHALLENGE                = 0x2001
	TLV_SECURITY_ASSOCIATION_UPDATE             = 0x2002
	TLV_CUM_FREQ_SCALE_FACTOR_OFFSET            = 0x2003
)

// Flag Field Bits
const (
	FLAG_LEAP_61        = 1 << 0 // flagField[1]
	FLAG_LEAP_59        = 1 << 1 // flagField[1]
	FLAG_UTC_OFF_VALID  = 1 << 2 // flagField[1]
	FLAG_PTP_TIMESCALE  = 1 << 3 // flagField[1]
	FLAG_TIME_TRACEABLE = 1 << 4 // flagField[1]
	FLAG_FREQ_TRACEABLE = 1 << 5 // flagField[1]
	FLAG_SYNC_UNCERTAIN = 1 << 6 // flagField[1]

	FLAG_ALT_MASTER = 1 << 0 // flagField[0]
	FLAG_TWO_STEP   = 1 << 1 // flagField[0]
	FLAG_UNICAST    = 1 << 2 // flagField[0]
)

// Notification Events (from linuxptp notification.h)
const (
	NOTIFY_PORT_STATE      = 1 << 0 // Port state changes (PORT_DATA_SET notifications)
	NOTIFY_TIME_SYNC       = 1 << 1 // Time synchronization events (TIME_STATUS_NP notifications)
	NOTIFY_PARENT_DATA_SET = 1 << 2 // Parent data set changes (PARENT_DATA_SET notifications)
	// Additional notification types that may be available:
	// NOTIFY_CMLDS = 1 << 3            // CMLDS events
)

// Core PTP Data Types
type ClockIdentity [8]byte
type PortIdentity struct {
	ClockIdentity ClockIdentity
	PortNumber    uint16
}

type Timestamp struct {
	SecondsField     uint64 // 48 bits
	NanosecondsField uint32
}

type ClockQuality struct {
	ClockClass              uint8
	ClockAccuracy           uint8
	OffsetScaledLogVariance uint16
}

// PTP Header
type PTPHeader struct {
	TransportSpecificMessageType uint8 // transportSpecific (4 bits) | messageType (4 bits)
	VersionPTP                   uint8 // minorVersionPTP (4 bits) | versionPTP (4 bits)
	MessageLength                uint16
	DomainNumber                 uint8
	Reserved1                    uint8
	FlagField                    [2]uint8
	Correction                   int64
	Reserved2                    uint32
	SourcePortIdentity           PortIdentity
	SequenceID                   uint16
	Control                      uint8
	LogMessageInterval           int8
}

// Management Message
type ManagementMessage struct {
	Header               PTPHeader
	TargetPortIdentity   PortIdentity
	StartingBoundaryHops uint8
	BoundaryHops         uint8
	ActionField          uint8 // reserved (4 bits) | actionField (4 bits)
	Reserved             uint8
}

// TLV Header
type TLVHeader struct {
	Type   uint16
	Length uint16
}

// Management TLV
type ManagementTLV struct {
	Type         uint16
	Length       uint16
	ManagementID uint16
	Data         []byte
}

// Default Data Set
type DefaultDataSet struct {
	TwoStepFlag   bool
	SlaveOnly     bool
	NumberPorts   uint16
	Priority1     uint8
	ClockQuality  ClockQuality
	Priority2     uint8
	ClockIdentity ClockIdentity
	DomainNumber  uint8
}

// Port Data Set
type PortDataSet struct {
	PortIdentity            PortIdentity
	PortState               uint8
	LogMinDelayReqInterval  int8
	PeerMeanPathDelay       int64
	LogAnnounceInterval     int8
	AnnounceReceiptTimeout  uint8
	LogSyncInterval         int8
	DelayMechanism          uint8
	LogMinPdelayReqInterval int8
	VersionNumber           uint8
}

// Time Properties Data Set
type TimePropertiesDataSet struct {
	CurrentUtcOffset      int16
	CurrentUtcOffsetValid bool
	Leap59                bool
	Leap61                bool
	TimeTraceable         bool
	FrequencyTraceable    bool
	PtpTimescale          bool
	TimeSource            uint8
}

// Current Data Set
type CurrentDataSet struct {
	StepsRemoved     uint16
	OffsetFromMaster int64 // nanoseconds
	MeanPathDelay    int64 // nanoseconds
}

// Parent Data Set
type ParentDataSet struct {
	ParentPortIdentity                    PortIdentity
	ParentStats                           bool
	ObservedParentOffsetScaledLogVariance uint16
	ObservedParentClockPhaseChangeRate    int32
	GrandmasterIdentity                   ClockIdentity
	GrandmasterClockQuality               ClockQuality
	GrandmasterPriority1                  uint8
	GrandmasterPriority2                  uint8
}

// Grandmaster Settings (Non-standard)
type GrandmasterSettingsNP struct {
	ClockQuality ClockQuality
	UtcOffset    int16
	TimeFlags    uint8
	TimeSource   uint8
}

// External Grandmaster Properties (Non-standard)
type ExternalGrandmasterPropertiesNP struct {
	GmIdentity   ClockIdentity
	StepsRemoved uint16
}

// Time Status (Non-standard) - contains timing synchronization information
type TimeStatusNP struct {
	MasterOffset               int64         // Offset from master in nanoseconds
	IngressTime                int64         // Ingress timestamp in nanoseconds
	CumulativeScaledRateOffset int32         // Cumulative scaled rate offset
	ScaledLastGmPhaseChange    int32         // Scaled last GM phase change
	GmTimeBaseIndicator        uint16        // GM time base indicator
	LastGmPhaseChange          ClockIdentity // Last GM phase change
	GmPresent                  bool          // GM present flag
	GmIdentity                 ClockIdentity // GM identity
}

// Port Properties (Non-standard)
type PortPropertiesNP struct {
	PortIdentity PortIdentity
	PortState    uint8
	Timestamping uint8
	Interface    PTPText
}

// PTP Text
type PTPText struct {
	Length uint8
	Text   []byte
}

// Port Hardware Clock (Non-standard)
type PortHardwareClockNP struct {
	PortIdentity PortIdentity
	PhcIndex     int32
}

// Subscribe Events (Non-standard)
type SubscribeEventsNP struct {
	Duration uint16
	Bitmask  uint16
}

// Utility functions
func (ci ClockIdentity) String() string {
	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x:%02x:%02x",
		ci[0], ci[1], ci[2], ci[3], ci[4], ci[5], ci[6], ci[7])
}

func (pi PortIdentity) String() string {
	return fmt.Sprintf("%s/%d", pi.ClockIdentity.String(), pi.PortNumber)
}

func (t Timestamp) String() string {
	return fmt.Sprintf("%d.%09d", t.SecondsField, t.NanosecondsField)
}

func PortStateString(state uint8) string {
	switch state {
	case PS_INITIALIZING:
		return "INITIALIZING"
	case PS_FAULTY:
		return "FAULTY"
	case PS_DISABLED:
		return "DISABLED"
	case PS_LISTENING:
		return "LISTENING"
	case PS_PRE_MASTER:
		return "PRE_MASTER"
	case PS_MASTER:
		return "MASTER"
	case PS_PASSIVE:
		return "PASSIVE"
	case PS_UNCALIBRATED:
		return "UNCALIBRATED"
	case PS_SLAVE:
		return "SLAVE"
	default:
		return "UNKNOWN"
	}
}

// Binary encoding/decoding helpers
func (h *PTPHeader) Encode(buf []byte) {
	binary.BigEndian.PutUint16(buf[2:4], h.MessageLength)
	buf[0] = h.TransportSpecificMessageType
	buf[1] = h.VersionPTP
	buf[4] = h.DomainNumber
	buf[5] = h.Reserved1
	buf[6] = h.FlagField[0]
	buf[7] = h.FlagField[1]
	binary.BigEndian.PutUint64(buf[8:16], uint64(h.Correction))
	binary.BigEndian.PutUint32(buf[16:20], h.Reserved2)
	copy(buf[20:28], h.SourcePortIdentity.ClockIdentity[:])
	binary.BigEndian.PutUint16(buf[28:30], h.SourcePortIdentity.PortNumber)
	binary.BigEndian.PutUint16(buf[30:32], h.SequenceID)
	buf[32] = h.Control
	buf[33] = uint8(h.LogMessageInterval)
}

func (h *PTPHeader) Decode(buf []byte) {
	h.TransportSpecificMessageType = buf[0]
	h.VersionPTP = buf[1]
	h.MessageLength = binary.BigEndian.Uint16(buf[2:4])
	h.DomainNumber = buf[4]
	h.Reserved1 = buf[5]
	h.FlagField[0] = buf[6]
	h.FlagField[1] = buf[7]
	h.Correction = int64(binary.BigEndian.Uint64(buf[8:16]))
	h.Reserved2 = binary.BigEndian.Uint32(buf[16:20])
	copy(h.SourcePortIdentity.ClockIdentity[:], buf[20:28])
	h.SourcePortIdentity.PortNumber = binary.BigEndian.Uint16(buf[28:30])
	h.SequenceID = binary.BigEndian.Uint16(buf[30:32])
	h.Control = buf[32]
	h.LogMessageInterval = int8(buf[33])
}

// Message represents a complete PTP management message
type Message struct {
	TLV     *ManagementTLV
	RawData []byte
}

// Decode deserializes a binary message
func (m *Message) Decode(buf []byte) error {
	if len(buf) < 48 {
		return fmt.Errorf("message too short: %d bytes", len(buf))
	}

	// TLV starts at offset 48 for management messages
	offset := 48
	if len(buf) < offset+6 {
		return fmt.Errorf("message too short for TLV header")
	}

	m.TLV = &ManagementTLV{}
	m.TLV.Type = binary.BigEndian.Uint16(buf[offset : offset+2])
	m.TLV.Length = binary.BigEndian.Uint16(buf[offset+2 : offset+4])
	m.TLV.ManagementID = binary.BigEndian.Uint16(buf[offset+4 : offset+6])

	// Extract data if present
	dataLength := int(m.TLV.Length) - 2
	if dataLength > 0 {
		if len(buf) < offset+6+dataLength {
			return fmt.Errorf("message too short for TLV data")
		}
		m.TLV.Data = make([]byte, dataLength)
		copy(m.TLV.Data, buf[offset+6:offset+6+dataLength])
	}

	m.RawData = make([]byte, len(buf))
	copy(m.RawData, buf)
	return nil
}
