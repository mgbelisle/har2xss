package main

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strings"
)

var usagePrefix = fmt.Sprintf(`Reads a .har file from stdin, prints all request parameters that are reflected in the response body to stdout

Usage: %s [OPTIONS]

OPTIONS:
`, os.Args[0])

var domainsFlag = flag.String("domains", "", "Filter by space delimited list of domains")

type KeyValue struct {
	Key   []string `json:"key"` // Keys can be nested e.g. person.parent.name
	Value string   `json:"value"`
}

func main() {
	// Flag setup
	flag.Usage = func() {
		fmt.Fprint(os.Stdout, usagePrefix)
		flag.PrintDefaults()
	}
	flag.Parse()

	// Parse the .har file
	har := struct {
		Log struct {
			Entries []struct {
				Request struct {
					Method      string `json:"method"`
					URL         string `json:"url"`
					QueryString []struct {
						Name  string `json:"name"`
						Value string `json:"value"`
					} `json:"queryString"`
					PostData struct {
						Params []struct {
							Name  string `json:"name"`
							Value string `json:"value"`
						} `json:"params"`
						Text string `json:"text"`
					} `json:"postData"`
				} `json:"request"`
				Response struct {
					Content struct {
						Text string `json:"text"`
					} `json:"content"`
				} `json:"response"`
			} `json:"entries"`
		} `json:"log"`
	}{}
	if err := json.NewDecoder(os.Stdin).Decode(&har); err != nil {
		panic(err)
	}

	domains := strings.Fields(*domainsFlag)
	results := []interface{}{}
	for _, entry := range har.Log.Entries {
		keyValueChan := make(chan *KeyValue)
		go func() {
			defer close(keyValueChan)

			// Search query params
			for _, queryString := range entry.Request.QueryString {
				for keyValue := range search(
					[]string{"query", queryString.Name},
					queryString.Value,
				) {
					keyValueChan <- keyValue
				}
			}

			// Search post params
			for _, param := range entry.Request.PostData.Params {
				for keyValue := range search(
					[]string{"form", param.Name},
					param.Value,
				) {
					keyValueChan <- keyValue
				}
			}

			// Search body
			for keyValue := range search([]string{"body"}, entry.Request.PostData.Text) {
				keyValueChan <- keyValue
			}
		}()
		if 0 < len(domains) {
			u, err := url.Parse(entry.Request.URL)
			if err != nil {
				panic(err)
			}
			ok := false
			for _, domain := range domains {
				if domain == u.Host {
					ok = true
					break
				}
			}
			if !ok {
				continue
			}
		}
		respBody, err := base64.StdEncoding.DecodeString(entry.Response.Content.Text)
		if err != nil {
			panic(err)
		}
		respBodyString := string(respBody)

		keyValues := []*KeyValue{}
		for keyValue := range keyValueChan {
			// TODO: Filter content type
			if strings.Contains(respBodyString, keyValue.Value) {
				keyValues = append(keyValues, keyValue)
			}
		}
		results = append(results, struct {
			Method string      `json:"method"`
			URL    string      `json:"url"`
			XSS    []*KeyValue `json:"xss"`
		}{
			Method: entry.Request.Method,
			URL:    entry.Request.URL,
			XSS:    keyValues,
		})
	}
	if err := json.NewEncoder(os.Stdout).Encode(results); err != nil {
		panic(err)
	}
}

// Recursive key value search
func search(key []string, value string) <-chan *KeyValue {
	keyValueChan := make(chan *KeyValue)
	go func() {
		defer close(keyValueChan)
		valueBytes := []byte(value)

		// Maybe a json map
		valueMap := map[string]json.RawMessage{}
		if err := json.Unmarshal(valueBytes, &valueMap); err == nil {
			for key2, value2 := range valueMap {
				for keyValue := range search(append(key, key2), string(value2)) {
					keyValueChan <- keyValue
				}
			}
		}

		// Maybe a json list
		valueList := []json.RawMessage{}
		if err := json.Unmarshal(valueBytes, &valueList); err == nil {
			for key2, value2 := range valueList {
				for keyValue := range search(append(key, fmt.Sprintf("%d", key2)), string(value2)) {
					keyValueChan <- keyValue
				}
			}
		}

		// Maybe a json string
		valueString := ""
		if err := json.Unmarshal(valueBytes, &valueString); err == nil {
			for keyValue := range search(key, valueString) {
				keyValueChan <- keyValue
			}
		}

		// Maybe base64 encoded
		if bytes, _ := base64.StdEncoding.DecodeString(value); 0 < len(bytes) {
			// TODO: Maybe check this
			// isPrint
			// for _, r := range string(bytes) {
			// 	if !unicode.IsPrint(r) {
			// 		return
			// 	}
			// }
			for keyValue := range search(key, string(bytes)) {
				keyValueChan <- keyValue
			}
		}

		keyValueChan <- &KeyValue{
			Key:   key,
			Value: value,
		}
	}()
	return keyValueChan
}
