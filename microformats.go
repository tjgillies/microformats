// microformats project microformats.go
package microformats

import (
	"bytes"
	"io"
	"regexp"
	"strings"
	//	"encoding/json"
	"net/url"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

var (
	RootClassNames     = regexp.MustCompile("^h-\\S*$")
	PropertyClassNames = regexp.MustCompile("^(p|u|dt|e)-(\\S*)$")
)

type MicroFormat struct {
	Value      string                   `json:"value,omitempty"`
	HTML       string                   `json:"html,omitempty"`
	Type       []string                 `json:"type"`
	Properties map[string][]interface{} `json:"properties"`
	Shape      string                   `json:"shape,omitempty"`
	Coords     string                   `json:"coords,omitempty"`
	Children   []*MicroFormat           `json:"children,omitempty"`
}

type Parser struct {
	curData   *Data
	curItem   *MicroFormat
	base      *url.URL
	baseFound bool
}

type Data struct {
	Items      []*MicroFormat      `json:"items"`
	Rels       map[string][]string `json:"rels"`
	Alternates []*AlternateRel     `json:"alternates,omitempty"`
	RelURLs    map[string]*RelURL  `json:"rel-urls,omitempty"`
}

type AlternateRel struct {
	URL      string `json:"url,omitempty"`
	Rel      string `json:"rel,omitempty"`
	Media    string `json:"media,omitempty"`
	HrefLang string `json:"hreflang,omitempty"`
	Type     string `json:"type,omitempty"`
}

type RelURL struct {
	Rels     []string `json:"rels,omitempty"`
	Text     string   `json:"text,omitempty"`
	Media    string   `json:"media,omitempty"`
	HrefLang string   `json:"hreflang,omitempty"`
	Type     string   `json:"type,omitempty"`
}

func New() *Parser {
	return &Parser{}
}

func (p *Parser) Parse(r io.Reader, baseURL *url.URL) *Data {
	doc, _ := html.Parse(r)
	return p.ParseNode(doc, baseURL)
}

func (p *Parser) ParseNode(doc *html.Node, baseURL *url.URL) *Data {
	p.curData = &Data{
		Items:   make([]*MicroFormat, 0),
		Rels:    make(map[string][]string),
		RelURLs: make(map[string]*RelURL),
	}
	p.base = baseURL
	p.baseFound = false
	p.walk(doc)
	return p.curData
}

func (p *Parser) walk(node *html.Node) {
	var curItem *MicroFormat
	var priorItem *MicroFormat
	rootclasses := make([]string, 0)
	classes := GetClasses(node)
	for _, class := range classes {
		if RootClassNames.MatchString(class) {
			rootclasses = append(rootclasses, class)
		}
	}
	if len(rootclasses) > 0 {
		curItem = &MicroFormat{}
		curItem.Type = rootclasses
		curItem.Properties = make(map[string][]interface{})
		if p.curItem == nil {
			p.curData.Items = append(p.curData.Items, curItem)
		}
		priorItem = p.curItem
		p.curItem = curItem
	}
	if !p.baseFound && isAtom(node, atom.Base) {
		if GetAttr(node, "href") != "" {
			newbase, _ := url.Parse(GetAttr(node, "href"))
			newbase = p.base.ResolveReference(newbase)
			p.base = newbase
			p.baseFound = true
		}
	}

	if isAtom(node, atom.A, atom.Link) {
		if rel := GetAttr(node, "rel"); rel != "" {
			urlVal := GetAttr(node, "href")

			if p.base != nil {
				urlParsed, _ := url.Parse(urlVal)
				urlParsed = p.base.ResolveReference(urlParsed)
				urlVal = urlParsed.String()
			}

			rels := strings.Split(rel, " ")
			alternate := false
			for i, relval := range rels {
				if relval == "alternate" {
					alternate = true
					rels = append(rels[:i], rels[i+1:]...)
					break
				}
			}
			if !alternate {
				for _, relval := range rels {
					p.curData.Rels[relval] = append(p.curData.Rels[relval], urlVal)
				}
				p.curData.RelURLs[urlVal] = &RelURL{
					Text:     getTextContent(node),
					Rels:     rels,
					Media:    GetAttr(node, "media"),
					HrefLang: GetAttr(node, "hreflang"),
					Type:     GetAttr(node, "type"),
				}
			} else {
				relstring := strings.Join(rels, " ")
				p.curData.Alternates = append(p.curData.Alternates, &AlternateRel{
					URL:      urlVal,
					Rel:      relstring,
					Media:    GetAttr(node, "media"),
					HrefLang: GetAttr(node, "hreflang"),
					Type:     GetAttr(node, "type"),
				})
			}
		}
	}

	for c := node.FirstChild; c != nil; c = c.NextSibling {
		p.walk(c)
	}

	if curItem != nil {
		if _, ok := curItem.Properties["name"]; !ok {
			name := p.getImpliedName(node)
			if name != "" {
				curItem.Properties["name"] = append(curItem.Properties["name"], name)
			}
		}
		if _, ok := curItem.Properties["photo"]; !ok {
			photo := p.getImpliedPhoto(node)
			if photo != "" {
				curItem.Properties["photo"] = append(curItem.Properties["photo"], photo)
			}
		}
		if _, ok := curItem.Properties["url"]; !ok {
			url := p.getImpliedURL(node)
			if url != "" {
				curItem.Properties["url"] = append(curItem.Properties["url"], url)
			}
		}
		p.curItem = priorItem
	}
	propertyclasses := make([][]string, 0)
	for _, class := range classes {
		match := PropertyClassNames.FindStringSubmatch(class)
		if match != nil {
			propertyclasses = append(propertyclasses, match)
		}
	}
	if len(propertyclasses) > 0 {
		for _, prop := range propertyclasses {

			var value *string
			var htmlbody string
			switch prop[1] {
			case "p":
				value = p.getValueClassPattern(node)
				if value == nil && isAtom(node, atom.Abbr) {
					value = getAttrPtr(node, "title")
				}
				if value == nil && isAtom(node, atom.Data, atom.Input) {
					value = getAttrPtr(node, "value")
				}
				if value == nil && isAtom(node, atom.Img, atom.Area) {
					value = getAttrPtr(node, "alt")
				}
				if value == nil {
					value = new(string)
					*value = getTextContent(node)
				}
			case "u":
				if value == nil && isAtom(node, atom.A, atom.Area) {
					value = getAttrPtr(node, "href")
				}
				if value == nil && isAtom(node, atom.Img, atom.Audio, atom.Video, atom.Source) {
					value = getAttrPtr(node, "src")
				}
				if value == nil && isAtom(node, atom.Object) {
					value = getAttrPtr(node, "data")
				}
				if p.base != nil && value != nil {
					urlParsed, _ := url.Parse(*value)
					urlParsed = p.base.ResolveReference(urlParsed)
					*value = urlParsed.String()
				}
				// TODO: value-class-pattern
				if value == nil && isAtom(node, atom.Abbr) {
					value = getAttrPtr(node, "title")
				}
				if value == nil && isAtom(node, atom.Data, atom.Input) {
					value = getAttrPtr(node, "value")
				}
				if value == nil {
					value = new(string)
					*value = getTextContent(node)
				}
			case "e":
				value = new(string)
				*value = getTextContent(node)
				buf := &bytes.Buffer{}
				for c := node.FirstChild; c != nil; c = c.NextSibling {
					html.Render(buf, c)
				}
				htmlbody = buf.String()
			case "dt":
				if value == nil {
					value = p.getValueClassPattern(node)
				}
				if value == nil && isAtom(node, atom.Time, atom.Ins, atom.Del) {
					value = getAttrPtr(node, "datetime")
				}
				if value == nil && isAtom(node, atom.Abbr) {
					value = getAttrPtr(node, "title")
				}
				if value == nil && isAtom(node, atom.Data, atom.Input) {
					value = getAttrPtr(node, "value")
				}
			}
			if curItem != nil && p.curItem != nil {
				p.curItem.Properties[prop[2]] = append(p.curItem.Properties[prop[2]], &MicroFormat{
					Type:       curItem.Type,
					Properties: curItem.Properties,
					Coords:     curItem.Coords,
					Shape:      curItem.Shape,
					Value:      *value,
					HTML:       htmlbody,
				})
			} else if value != nil && *value != "" && p.curItem != nil {
				if htmlbody != "" {
					p.curItem.Properties[prop[2]] = append(p.curItem.Properties[prop[2]], map[string]interface{}{"value": *value, "html": htmlbody})
				} else {
					p.curItem.Properties[prop[2]] = append(p.curItem.Properties[prop[2]], *value)
				}
			}
		}
	} else {
		if curItem != nil && p.curItem != nil {
			p.curItem.Children = append(p.curItem.Children, curItem)
		}
	}
}

func GetClasses(node *html.Node) []string {
	for _, attr := range node.Attr {
		if strings.EqualFold(attr.Key, "class") {
			return strings.Split(attr.Val, " ")
		}
	}
	return []string{}
}

func HasMatchingClass(node *html.Node, regex *regexp.Regexp) bool {
	classes := GetClasses(node)
	for _, class := range classes {
		if regex.MatchString(class) {
			return true
		}
	}
	return false
}

func GetAttr(node *html.Node, name string) string {
	for _, attr := range node.Attr {
		if strings.EqualFold(attr.Key, name) {
			return attr.Val
		}
	}
	return ""
}

func getAttrPtr(node *html.Node, name string) *string {
	for _, attr := range node.Attr {
		if strings.EqualFold(attr.Key, name) {
			return &attr.Val
		}
	}
	return nil
}

func isAtom(node *html.Node, atoms ...atom.Atom) bool {
	for _, atom := range atoms {
		if atom == node.DataAtom {
			return true
		}
	}
	return false
}

func ParseValueClass(node *html.Node) string {

	return ""
}
