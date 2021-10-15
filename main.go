package main

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"unicode"
)

var usagePrefix = fmt.Sprintf(`Given a list of .har files, prints all request parameters that are reflected in the response body

Usage: %s [OPTIONS] [HAR_FILE ...]

OPTIONS:
`, os.Args[0])

func main() {
	// Flag setup
	flag.Usage = func() {
		fmt.Fprint(os.Stdout, usagePrefix)
		flag.PrintDefaults()
	}
	flag.Parse()

	if len(flag.Args()) == 0 {
		print(os.Stdin, log.New(os.Stderr, "", 0))
	} else {
		for _, fpath := range flag.Args() {
			file, err := os.Open(fpath)
			if err != nil {
				panic(err)
			}
			func() {
				defer file.Close()
				print(file, log.New(os.Stderr, fpath+": ", 0))
			}()
		}
	}
}

func printParam(key, value, request string) {
	fmt.Printf("key: %s\nvalue: %s\nrequest: %s\n\n", key, value, request)
	printParamRecurse(key, value, request)
}

// Returns a json like key string a["b"]
func makeKey(a, b string) string {
	bJSON, _ := json.Marshal([]string{b})
	return a + string(bJSON)
}

func isPrint(s string) bool {
	for _, r := range s {
		if !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

func printParamRecurse(key, value, request string) {
	// Maybe a json map
	valueMap := map[string]json.RawMessage{}
	if err := json.Unmarshal([]byte(value), &valueMap); err == nil {
		for key2, value2 := range valueMap {
			printParam(makeKey(key, key2), string(value2), request)
		}
		return
	}

	// Maybe a json list
	valueList := []json.RawMessage{}
	if err := json.Unmarshal([]byte(value), &valueList); err == nil {
		for _, value2 := range valueList {
			printParam(fmt.Sprintf("%s[]", key), string(value2), request)
		}
		return
	}

	// Maybe a json string
	valueString := ""
	if err := json.Unmarshal([]byte(value), &valueString); err == nil {
		printParamRecurse(key, valueString, request)
		return
	}

	// Maybe base64 encoded
	if bytes, err := base64.StdEncoding.DecodeString(value); err == nil && 0 < len(bytes) && isPrint(string(bytes)) {
		printParam(key, string(bytes), request)
		return
	}
}

func print(reader io.Reader, logger *log.Logger) {
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
			} `json:"entries"`
		} `json:"log"`
	}{}
	if err := json.NewDecoder(reader).Decode(&har); err != nil {
		logger.Panic(err)
	}

	makeRequest := func(requestMethod, requestURL string) string {
		u, err := url.Parse(requestURL)
		if err != nil {
			logger.Panic(err)
		}
		u.RawQuery = ""
		return fmt.Sprintf("%s %s", requestMethod, u.String())
	}
	for _, entry := range har.Log.Entries {
		request := makeRequest(entry.Request.Method, entry.Request.URL)
		// Print the query params
		for _, queryString := range entry.Request.QueryString {
			printParam(
				makeKey("query", queryString.Name),
				queryString.Value,
				request,
			)
		}
		for _, param := range entry.Request.PostData.Params {
			printParam(
				makeKey("form", param.Name),
				param.Value,
				request,
			)
		}
		printParamRecurse("body", entry.Request.PostData.Text, request)
	}
}
