package escp

// Generated while extracting the model package: the tests of this package use the names of the model
// unqualified, as they did before the extraction.

import "github.com/davidlukac/escp-go/model"

type BuildDate = model.BuildDate
type ConfigFile = model.ConfigFile

var ConfigurableSpeeds = model.ConfigurableSpeeds

type Dot1Q = model.Dot1Q
type Dot1QVLAN = model.Dot1QVLAN

type Firmware = model.Firmware
type Group = model.Group
type Hostname = model.Hostname
type IGMP = model.IGMP
type IGMPConfig = model.IGMPConfig
type IGMPGroup = model.IGMPGroup
type IPConfig = model.IPConfig
type Impact = model.Impact

const ImpactCosmetic = model.ImpactCosmetic
const ImpactManagement = model.ImpactManagement
const ImpactTraffic = model.ImpactTraffic

type Info = model.Info
type Kbps = model.Kbps
type LAG = model.LAG

const LAGGroupCount = model.LAGGroupCount

type LAGID = model.LAGID

const LAGMinPorts = model.LAGMinPorts
const LAGPortsPerGroup = model.LAGPortsPerGroup

type MAC = model.MAC

var MACFromBytes = model.MACFromBytes

type MTUVLAN = model.MTUVLAN

const MaxBandwidth = model.MaxBandwidth
const MaxHostnameLen = model.MaxHostnameLen
const MaxPort = model.MaxPort
const MaxPortVLANID = model.MaxPortVLANID
const MaxVLANID = model.MaxVLANID
const MaxVLANNameLen = model.MaxVLANNameLen

type Method = model.Method

const MethodBandwidth = model.MethodBandwidth
const MethodConfigBackup = model.MethodConfigBackup
const MethodDeleteDot1QVLAN = model.MethodDeleteDot1QVLAN
const MethodDot1Q = model.MethodDot1Q
const MethodEnableDHCP = model.MethodEnableDHCP
const MethodIGMP = model.MethodIGMP
const MethodInfo = model.MethodInfo
const MethodLAGs = model.MethodLAGs
const MethodLED = model.MethodLED
const MethodLoopPrevention = model.MethodLoopPrevention
const MethodMTUVLAN = model.MethodMTUVLAN
const MethodPortBasedVLAN = model.MethodPortBasedVLAN
const MethodPortMirror = model.MethodPortMirror
const MethodPortStats = model.MethodPortStats
const MethodPorts = model.MethodPorts
const MethodQoS = model.MethodQoS
const MethodSetBandwidth = model.MethodSetBandwidth
const MethodSetDot1QVLAN = model.MethodSetDot1QVLAN
const MethodSetHostname = model.MethodSetHostname
const MethodSetIGMP = model.MethodSetIGMP
const MethodSetIPConfig = model.MethodSetIPConfig
const MethodSetLAG = model.MethodSetLAG
const MethodSetLED = model.MethodSetLED
const MethodSetLoopPrevention = model.MethodSetLoopPrevention
const MethodSetPVIDs = model.MethodSetPVIDs
const MethodSetPort = model.MethodSetPort
const MethodSetPortBasedVLAN = model.MethodSetPortBasedVLAN
const MethodSetPortMirror = model.MethodSetPortMirror
const MethodSetPortPriorities = model.MethodSetPortPriorities
const MethodSetQoSMode = model.MethodSetQoSMode
const MethodSetStormControl = model.MethodSetStormControl
const MethodSetVLANMode = model.MethodSetVLANMode
const MethodStormControl = model.MethodStormControl
const MethodSystem = model.MethodSystem
const MethodVLANMode = model.MethodVLANMode
const MinVLANID = model.MinVLANID

type Mirror = model.Mirror

var MustPortSet = model.MustPortSet
var NewPortSet = model.NewPortSet

type Operation = model.Operation

var ParseBuildDate = model.ParseBuildDate
var ParseFirmware = model.ParseFirmware
var ParseIPv4 = model.ParseIPv4
var ParseImpact = model.ParseImpact
var ParseMAC = model.ParseMAC
var ParseMethod = model.ParseMethod
var ParsePortID = model.ParsePortID
var ParsePortMask = model.ParsePortMask
var ParseQoSMode = model.ParseQoSMode
var ParseSpeed = model.ParseSpeed
var ParseVLANMode = model.ParseVLANMode

type Password = model.Password
type Port = model.Port
type PortBandwidth = model.PortBandwidth
type PortBasedVLAN = model.PortBasedVLAN
type PortConfig = model.PortConfig
type PortID = model.PortID
type PortPVID = model.PortPVID
type PortPriority = model.PortPriority
type PortSet = model.PortSet
type PortStats = model.PortStats
type PortStorm = model.PortStorm
type PortVLAN = model.PortVLAN
type PortVLANID = model.PortVLANID
type PortVLANLimit = model.PortVLANLimit
type Priority = model.Priority

const PriorityHighest = model.PriorityHighest
const PriorityLowest = model.PriorityLowest

type QoS = model.QoS

const QoS8021p = model.QoS8021p
const QoSDSCP8021p = model.QoSDSCP8021p

type QoSMode = model.QoSMode

var QoSModes = model.QoSModes

const QoSPortBased = model.QoSPortBased
const RateStep = model.RateStep

type Reader = model.Reader
type Setting = model.Setting

var Settings = model.Settings

type Speed = model.Speed

const Speed1000Full = model.Speed1000Full
const Speed100Full = model.Speed100Full
const Speed100Half = model.Speed100Half
const Speed10Full = model.Speed10Full
const Speed10Half = model.Speed10Half
const SpeedAuto = model.SpeedAuto
const SpeedLinkDown = model.SpeedLinkDown

type StormConfig = model.StormConfig
type System = model.System
type VLANID = model.VLANID
type VLANMode = model.VLANMode

const VLANModeDot1Q = model.VLANModeDot1Q

var VLANModeFrom = model.VLANModeFrom

const VLANModeMTU = model.VLANModeMTU
const VLANModeNone = model.VLANModeNone
const VLANModePortBased = model.VLANModePortBased

type VLANModeSetting = model.VLANModeSetting

var VLANModes = model.VLANModes

type VLANName = model.VLANName
type WriteGate = model.WriteGate
type WriteMethod = model.WriteMethod
