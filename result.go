package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type URI struct {
	Namespace string
	Value     string
}

func (u URI) String() string {
	if u.Namespace != "" {
		return u.Namespace + "#" + u.Value
	}
	return u.Value
}

func (u URI) Bytes() []byte {
	return []byte(u.Namespace + "#" + u.Value)
}

func (u URI) IsVariable() bool {
	return strings.HasPrefix(u.Value, "?")
}

func ParseURI(uri string) URI {
	uri = strings.TrimLeft(uri, "<")
	uri = strings.TrimRight(uri, ">")
	parts := strings.Split(uri, "#")
	parts[0] = strings.TrimRight(parts[0], "#")
	if len(parts) != 2 {
		// try to parse ":"
		parts = strings.SplitN(uri, ":", 2)
		if len(parts) > 1 {
			return URI{Namespace: parts[0], Value: parts[1]}
		}
		return URI{Value: uri}
	}
	return URI{Namespace: parts[0], Value: parts[1]}
}

type Response struct {
	Results Results
	Head    Head
}

type Head struct {
	Vars []string
}

type Results struct {
	Bindings []map[string]interface{}
}

func ParseResponse(r io.Reader) ([][]URI, error) {
	dec := json.NewDecoder(r)
	resp := new(Response)
	err := dec.Decode(resp)

	results := [][]URI{}

	for _, bdg := range resp.Results.Bindings {
		row := []URI{}
		for _, varname := range resp.Head.Vars {
			vars, found := bdg[varname]
			if !found {
				break
			}

			row = append(row, ParseURI(vars.(map[string]interface{})["value"].(string)))
		}
		results = append(results, row)
	}

	fmt.Println("num results:", len(results))

	return results, err
}
