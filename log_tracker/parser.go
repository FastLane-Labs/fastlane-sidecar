package logtracker

type LogParser interface {
	ParseLog(line string) []LogType
}

type NilParser struct{}

func (p *NilParser) ParseLog(line string) []LogType {
	return []LogType{UnknownLog}
}
