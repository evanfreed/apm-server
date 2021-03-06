package sourcemap

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"sort"
	"strconv"
)

type v3 struct {
	sourceMap
	Sections []section `json:"sections"`
}

type sourceMap struct {
	Version    int           `json:"version"`
	File       string        `json:"file"`
	SourceRoot string        `json:"sourceRoot"`
	Sources    []string      `json:"sources"`
	Names      []interface{} `json:"names"`
	Mappings   string        `json:"mappings"`

	sourceRootURL *url.URL
	mappings      []mapping
}

func (m *sourceMap) parse(mapURL string) error {
	if err := checkVersion(m.Version); err != nil {
		return err
	}

	if m.SourceRoot != "" {
		u, err := url.Parse(m.SourceRoot)
		if err != nil {
			return err
		}
		if u.IsAbs() {
			m.sourceRootURL = u
		}
	} else if mapURL != "" {
		u, err := url.Parse(mapURL)
		if err != nil {
			return err
		}
		if u.IsAbs() {
			u.Path = path.Dir(u.Path)
			m.sourceRootURL = u
		}
	}

	mappings, err := parseMappings(m.Mappings)
	if err != nil {
		return err
	}

	m.mappings = mappings
	// Free memory.
	m.Mappings = ""

	return nil
}

func (m *sourceMap) absSource(source string) string {
	if path.IsAbs(source) {
		return source
	}

	if u, err := url.Parse(source); err == nil && u.IsAbs() {
		return source
	}

	if m.sourceRootURL != nil {
		u := *m.sourceRootURL
		u.Path = path.Join(m.sourceRootURL.Path, source)
		return u.String()
	}

	if m.SourceRoot != "" {
		return path.Join(m.SourceRoot, source)
	}

	return source
}

type section struct {
	Offset struct {
		Line   int `json:"line"`
		Column int `json:"column"`
	} `json:"offset"`
	Map *sourceMap `json:"map"`
}

type Consumer struct {
	file     string
	sections []section
}

func Parse(mapURL string, b []byte) (*Consumer, error) {
	v3 := new(v3)
	err := json.Unmarshal(b, v3)
	if err != nil {
		return nil, err
	}

	if err := checkVersion(v3.Version); err != nil {
		return nil, err
	}

	if len(v3.Sections) == 0 {
		v3.Sections = append(v3.Sections, section{
			Map: &v3.sourceMap,
		})
	}

	for _, s := range v3.Sections {
		err := s.Map.parse(mapURL)
		if err != nil {
			return nil, err
		}
	}

	reverse(v3.Sections)
	return &Consumer{
		file:     v3.File,
		sections: v3.Sections,
	}, nil
}

func (c *Consumer) File() string {
	return c.file
}

func (c *Consumer) Source(
	genLine, genColumn int,
) (source, name string, line, column int, ok bool) {
	for i := range c.sections {
		s := &c.sections[i]
		if s.Offset.Line < genLine ||
			(s.Offset.Line+1 == genLine && s.Offset.Column <= genColumn) {
			genLine -= s.Offset.Line
			genColumn -= s.Offset.Column
			return c.source(s.Map, genLine, genColumn)
		}
	}
	return
}

func (c *Consumer) source(
	m *sourceMap, genLine, genColumn int,
) (source, name string, line, column int, ok bool) {
	i := sort.Search(len(m.mappings), func(i int) bool {
		m := &m.mappings[i]
		if m.genLine == genLine {
			return m.genColumn >= genColumn
		}
		return m.genLine >= genLine
	})

	// Mapping not found.
	if i == len(m.mappings) {
		return
	}

	match := &m.mappings[i]

	// Fuzzy match.
	if match.genLine > genLine || match.genColumn > genColumn {
		if i == 0 {
			return
		}
		match = &m.mappings[i-1]
	}

	if match.sourcesInd >= 0 {
		source = m.absSource(m.Sources[match.sourcesInd])
	}
	if match.namesInd >= 0 {
		v := m.Names[match.namesInd]
		switch v := v.(type) {
		case string:
			name = v
		case float64:
			name = strconv.FormatFloat(v, 'f', -1, 64)
		default:
			name = fmt.Sprint(v)
		}
	}
	line = match.sourceLine
	column = match.sourceColumn
	ok = true
	return
}

func checkVersion(version int) error {
	if version == 3 || version == 0 {
		return nil
	}
	return fmt.Errorf(
		"sourcemap: got version=%d, but only 3rd version is supported",
		version,
	)
}

func reverse(ss []section) {
	last := len(ss) - 1
	for i := 0; i < len(ss)/2; i++ {
		ss[i], ss[last-i] = ss[last-i], ss[i]
	}
}
