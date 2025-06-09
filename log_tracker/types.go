package logtracker

type LogType int

const (
	UnknownLog LogType = iota
	ConsensusMessageReceived
	TimeoutReceived
	BlockReceived
	//...
)
