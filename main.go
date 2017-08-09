package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"

	confapi "github.com/seppestas/go-confluence"
	"golang.org/x/net/html"
)

type returnCheckerObj struct {
	url     string
	message string
	success bool
}

// HostURL is a given parameter which defines
// the destination environment for testing
var HostURL *string

func main() {
	// read command line parameters
	confluenceURL := flag.String("confluence-url", "https://blubb.atlassian.net/wiki/rest/api/space/ASD", "URL to the confluence content")
	confluenceContentID := flag.String("confluence-content-id", "2428384", "Content ID of the confluence page")
	confluenceUserName := flag.String("confluence-username", "", "Username for confluence api login")
	confluencePassword := flag.String("confluence-password", "", "Password for confluence api login")
	HostURL = flag.String("host-url", "https://google.com", "Environment URL which should be used to test the redirect url against it")
	flag.Parse()

	// get basic auth object and pass credentials
	basicAuth := confapi.BasicAuth(*confluenceUserName, *confluencePassword)

	// authenticate and get wiki object
	wiki, err := confapi.NewWiki(*confluenceURL, basicAuth)
	if err != nil {
		panic(err)
	}

	// get content from confluence
	expand := []string{"history", "space", "version", "body.storage"}
	content, err := wiki.GetContent(*confluenceContentID, expand)
	if err != nil {
		panic(err)
	}

	// print out the content for debug
	fmt.Printf("Confluence content get: %+v\n\n", content.Body.Storage.Value)

	// add html tag to beginning and end. Also create tokenizer from return string
	bodyValue := "<html>" + content.Body.Storage.Value + "</html>"
	z := html.NewTokenizer(strings.NewReader(bodyValue))

	// Parse content
	urlList := iterateResponse(z)

	// Check redirect rules
	c := make(chan returnCheckerObj)
	for _, url := range urlList {
		go func(url string) {
			c <- checkRedirect(url)
		}(url)
	}

	// Wait for all responses
	var returnObjSlice []returnCheckerObj
	for i := 0; i < len(urlList); i++ {
		returnObj := <-c
		returnObjSlice = append(returnObjSlice, returnObj)

		fmt.Printf("URL: %s\n", returnObj.url)
		fmt.Printf("    Success: %t\n", returnObj.success)
	}

	// Send results back to confluence
	var confluenceReturnData, singleTag string
	var foundURL = false
	for i := 0; i < len(content.Body.Storage.Value); i++ {
		singleChar := content.Body.Storage.Value[i : i+1]

		// add char to our tag variable
		singleTag += singleChar

		// Check if we have one complete tag by checking if the char was the closing char
		if singleChar == ">" {
			for _, singleObj := range returnObjSlice {
				switch {
				case strings.Contains(singleTag, singleObj.url):
					foundURL = true
				case strings.Contains(singleTag, "ac:emoticon") && foundURL:
					if singleObj.success {
						singleTag = strings.Replace(singleTag, "cross", "tick", -1)
					} else if !singleObj.success {
						singleTag = strings.Replace(singleTag, "tick", "cross", -1)
					}
					foundURL = false
				}
			}

			// Add complete tag to our final data variable and reset
			confluenceReturnData += singleTag
			singleTag = ""
		}
	}

	// print new confluence data for debug
	fmt.Printf("Confluence content push: %+v\n", confluenceReturnData)

	// save new return value, increment version and update confluence
	content.Body.Storage.Value = confluenceReturnData
	content.Version.Number++
	content, err = wiki.UpdateContent(content)
	if err != nil {
		panic(err)
	}
}

// checkRedirect opens a tcp connection to the given url.
// It checks if the response code is ok and if the response
// has a correct starting tag.
func checkRedirect(url string) (returnObj returnCheckerObj) {
	returnObj.url = url
	url = *HostURL + url

	response, err := http.Get(url)
	if err != nil {
		returnObj.message = err.Error()
		returnObj.success = false
		return
	}
	defer response.Body.Close()

	bodyBytes, err := ioutil.ReadAll(response.Body)
	if err != nil {
		returnObj.message = err.Error()
		returnObj.success = false
		return
	}

	bodyString := string(bodyBytes)
	returnObj.message = bodyString
	returnObj.success = true

	switch {
	case strings.HasSuffix(url, "?wsdl"):
		if !strings.Contains(bodyString, "<wsdl:definitions") {
			returnObj.message = "URL was ending with ?wsdl but did not contain <wsdl:definition!"
			returnObj.success = false
		}
	case strings.HasSuffix(url, ".xsd"):
		if !strings.Contains(bodyString, "<xs:schema") {
			returnObj.message = "URL was ending with .xsd but did not contain <xs:schema!"
			returnObj.success = false
		}
	}
	return
}

func iterateResponse(z *html.Tokenizer) (urlList []string) {
	for {
		tt := z.Next()
		tData := z.Token().Data

		switch {
		case tt == html.ErrorToken:
			return
		case tData == "td" && tt != html.EndTagToken:
			z.Next()
			tData = z.Token().Data

			// Check if it's a url
			if tData == "a" {
				// Get the value of the url
				z.Next()
				urlList = append(urlList, z.Token().Data)
			}
		}
	}
}
