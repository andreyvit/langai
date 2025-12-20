package langai

type Message struct {
	Role  Role
	Name  string
	Parts []*Part
}

type Part struct {
	Type PartType

	Text     string
	Image    *Image
	Document *Document

	ToolCall   *ToolCall
	ToolResult *ToolResult

	CacheControl CacheControl
}

type Image struct {
	MediaType string

	// Exactly one of these should be set:
	URL  string
	Data []byte
}

type Document struct {
	Name      string
	MediaType string
	Text      string
}

func SystemMsg(text string) Message    { return System(Text(text)) }
func UserMsg(text string) Message      { return User(Text(text)) }
func AssistantMsg(text string) Message { return Assistant(Text(text)) }

func System(parts ...*Part) Message    { return Message{Role: RoleSystem, Parts: parts} }
func User(parts ...*Part) Message      { return Message{Role: RoleUser, Parts: parts} }
func Assistant(parts ...*Part) Message { return Message{Role: RoleAssistant, Parts: parts} }

func Text(s string) *Part {
	return &Part{Type: PartText, Text: s}
}

func ImageURL(mediaType, url string) *Part {
	return &Part{Type: PartImage, Image: &Image{MediaType: mediaType, URL: url}}
}

func ImageBytes(mediaType string, data []byte) *Part {
	return &Part{Type: PartImage, Image: &Image{MediaType: mediaType, Data: data}}
}

func DocumentText(name, mediaType, text string) *Part {
	return &Part{Type: PartDocument, Document: &Document{Name: name, MediaType: mediaType, Text: text}}
}

func (p *Part) WithCacheControl(c CacheControl) *Part {
	p.CacheControl = c
	return p
}
