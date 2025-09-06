package recipes

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"
)

type Recipe struct {
	XMLName xml.Name `xml:"recipe"`
	Name    string   `xml:"name,attr"`
	System  string   `xml:"system"`
	Inputs  Inputs   `xml:"inputs"`
	Format  Format   `xml:"format"`
	Rules   Rules    `xml:"rules"`
}

type Inputs struct {
	Params []Param `xml:"param"`
}

type Param struct {
	Name     string `xml:"name,attr"`
	Required bool   `xml:"required,attr"`
	Source   string `xml:"source,attr"`
}

type Format struct {
	Heading  Heading   `xml:"heading"`
	Sections []Section `xml:"section"`
	Footer   Footer    `xml:"footer"`
}

type Heading struct {
	Level int       `xml:"level,attr"`
	Nodes []AnyNode `xml:",any"`
}

type Section struct {
	Title string    `xml:"title,attr"`
	Min   int       `xml:"min,attr"`
	Max   int       `xml:"max,attr"`
	Nodes []AnyNode `xml:",any"`
}

type Footer struct {
	Nodes []AnyNode `xml:",any"`
}

type Rules struct {
	Rule []string `xml:"rule"`
}

type AnyNode struct {
	XMLName  xml.Name
	Content  string    `xml:",chardata"`
	Children []AnyNode `xml:",any"`
	Ref      string    `xml:"ref,attr"`
}

func ExpandInline(nodes []AnyNode, vars map[string]string) (string, error) {
	var builder bytes.Buffer
	for _, node := range nodes {
		switch node.XMLName.Local {
		case "var":
			value, ok := vars[node.Ref]
			if !ok {
				return "", fmt.Errorf("missing variable: %s", node.Ref)
			}
			builder.WriteString(value)
		case "text":
			builder.WriteString(node.Content)
		default:
			if len(strings.TrimSpace(node.Content)) > 0 && len(node.Children) == 0 {
				builder.WriteString(node.Content)
			} else if len(node.Children) > 0 {
				expanded, err := ExpandInline(node.Children, vars)
				if err != nil {
					return "", err
				}
				builder.WriteString(expanded)
			}
		}
	}
	return builder.String(), nil
}
