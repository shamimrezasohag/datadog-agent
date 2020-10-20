package serverless

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"time"

	"github.com/DataDog/datadog-agent/pkg/dogstatsd"
)

const (
	extensionName = "datadog-agent"

	routeRegister      string = "http://localhost:9001/2020-01-01/extension/register"
	routeEventNext     string = "http://localhost:9001/2020-01-01/extension/event/next"
	routeInitError     string = "http://localhost:9001/2020-01-01/extension/init/error"
	routeSubscribeLogs string = "http://localhost:9001/2020-08-15/logs"

	headerExtName     string = "Lambda-Extension-Name"
	headerExtId       string = "Lambda-Extension-Identifier"
	headerExtErrType  string = "Lambda-Extension-Function-Error-Type"
	headerContentType string = "Content-Type"

	requestTimeout time.Duration = 5 * time.Second

	// FatalNoAPIKey is the error reported to the AWS Extension environment when
	// no API key has been set. Unused until we can report error
	// without stopping the extension.
	FatalNoAPIKey ErrorEnum = "Fatal.NoAPIKey"
	// FatalDogstatsdInit is the error reported to the AWS Extension environment when
	// DogStatsD fails to initialize properly. Unused until we can report error
	// without stopping the extension.
	FatalDogstatsdInit ErrorEnum = "Fatal.DogstatsdInit"
	// FatalBadEndpoint is the error reported to the AWS Extension environment when
	// bad endpoints have been configured. Unused until we can report error
	// without stopping the extension.
	FatalBadEndpoint ErrorEnum = "Fatal.BadEndpoint"
	// FatalBadEndpoint is the error reported to the AWS Extension environment when
	// a connection failed.
	FatalConnectFailed ErrorEnum = "Fatal.ConnectFailed"
)

// ID is the extension ID within the AWS Extension environment.
type ID string

// ErrorEnum are errors reported to the AWS Extension environment.
type ErrorEnum string

// String returns the string value for this ID.
func (i ID) String() string {
	return string(i)
}

// String returns the string value for this ErrorEnum.
func (e ErrorEnum) String() string {
	return string(e)
}

// Payload is the payload read in the response while subscribing to
// the AWS Extension env.
type Payload struct {
	EventType  string `json:"eventType"`
	DeadlineMs int64  `json:"deadlineMs"`
	//    RequestId string `json:"requestId"` // unused
}

// Register registers the serverless daemon and subscribe to INVOKE and SHUTDOWN messages.
// Returns either (the serverless ID assigned by the serverless daemon + the api key as read from
// the environment) or an error.
func Register() (ID, error) {
	var err error

	// create the POST register request
	// we will want to add here every configuration field that the serverless
	// agent supports.

	payload := bytes.NewBuffer(nil)
	payload.Write([]byte(`{"events":["INVOKE", "SHUTDOWN"]}`))

	var request *http.Request
	var response *http.Response

	if request, err = http.NewRequest(http.MethodPost, routeRegister, payload); err != nil {
		return "", fmt.Errorf("Register: can't create the POST register request: %v", err)
	}
	request.Header.Set(headerExtName, extensionName)

	// call the service to register and retrieve the given Id
	client := &http.Client{Timeout: 5 * time.Second}
	if response, err = client.Do(request); err != nil {
		return "", fmt.Errorf("Register: error while POST register route: %v", err)
	}

	// read the response
	// -----------------

	var body []byte
	if body, err = ioutil.ReadAll(response.Body); err != nil {
		return "", fmt.Errorf("Register: can't read the body: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != 200 {
		return "", fmt.Errorf("Register: didn't receive an HTTP 200: %v -- Response body content: %v", response.StatusCode, string(body))
	}

	// read the ID
	// -----------

	id := response.Header.Get(headerExtId)
	if len(id) == 0 {
		return "", fmt.Errorf("Register: didn't receive an identifier -- Response body content: %v", string(body))
	}

	return ID(id), nil
}

// SubscribeLogs subscribes to the logs collection on the platform.
// FIXME(remy): complete this comment (what is collected, how, ...)
func SubscribeLogs(id ID, httpAddr string) error {
	var err error
	var request *http.Request
	var response *http.Response
	var jsonBytes []byte

	if _, err := url.ParseRequestURI(httpAddr); err != nil || httpAddr == "" {
		return fmt.Errorf("SubscribeLogs: wrong http addr provided: %s", httpAddr)
	}

	// send a hit on a route to subscribe to the logs collection feature
	// --------------------

	if jsonBytes, err = json.Marshal(map[string]interface{}{
		"destination": map[string]string{
			"URI":      httpAddr,
			"protocol": "HTTP",
		},
		"types": []string{"platform", "extension", "function"}, // FIXME(remy): should be configurable
		"buffering": map[string]int{ // FIXME(remy): these should be better defined
			"timeoutMs": 1000,
			"maxBytes":  262144,
			"maxItems":  1000,
		},
	}); err != nil {
		return fmt.Errorf("SubscribeLogs: can't marshal subscribe JSON: %s", err)
	}

	if request, err = http.NewRequest(http.MethodPut, routeSubscribeLogs, bytes.NewBuffer(jsonBytes)); err != nil {
		return fmt.Errorf("SubscribeLogs: can't create the PUT request: %v", err)
	}
	request.Header.Set(headerExtId, id.String())
	request.Header.Set(headerContentType, "application/json")

	client := &http.Client{
		Transport: &http.Transport{IdleConnTimeout: requestTimeout},
		Timeout:   requestTimeout,
	}
	if response, err = client.Do(request); err != nil {
		return fmt.Errorf("SubscribeLogs: while PUT subscribe request: %s", err)
	}

	if response.StatusCode >= 300 {
		return fmt.Errorf("SubscribeLogs: received an HTTP %s", response.Status)
	}

	return nil
}

// ReportInitError reports an init error to the environment.
func ReportInitError(id ID, errorEnum ErrorEnum) error {
	var err error
	var content []byte
	var request *http.Request
	var response *http.Response

	if content, err = json.Marshal(map[string]string{
		"error": string(errorEnum),
	}); err != nil {
		return fmt.Errorf("ReportInitError: can't write the payload: %s", err)
	}

	if request, err = http.NewRequest(http.MethodPost, routeInitError, bytes.NewBuffer(content)); err != nil {
		return fmt.Errorf("ReportInitError: can't create the POST request: %s", err)
	}

	request.Header.Set(headerExtId, id.String())
	request.Header.Set(headerExtErrType, FatalConnectFailed.String())

	client := &http.Client{
		Transport: &http.Transport{IdleConnTimeout: requestTimeout},
		Timeout:   requestTimeout,
	}

	if response, err = client.Do(request); err != nil {
		return fmt.Errorf("ReportInitError: while POST init error route: %s", err)
	}

	if response.StatusCode >= 300 {
		return fmt.Errorf("ReportInitError: received an HTTP %s", response.Status)
	}

	return nil
}

// WaitForNextInvocation starts waiting and blocking until it receives a request.
// Note that for now, we only subscribe to INVOKE and SHUTDOWN messages.
// Write into stopCh to stop the main thread of the running program.
func WaitForNextInvocation(stopCh chan struct{}, statsdServer *dogstatsd.Server, id ID) error {
	var err error

	// do the blocking HTTP GET call

	var request *http.Request
	var response *http.Response

	if request, err = http.NewRequest(http.MethodGet, routeEventNext, nil); err != nil {
		return fmt.Errorf("WaitForNextInvocation: can't create the GET request: %v", err)
	}
	request.Header.Set(headerExtId, id.String())

	// the blocking call is here
	client := &http.Client{Timeout: 0} // this one should never timeout
	if response, err = client.Do(request); err != nil {
		return fmt.Errorf("WaitForNextInvocation: while GET next route: %v", err)
	}

	// we received a response, meaning we've been invoked

	var body []byte
	if body, err = ioutil.ReadAll(response.Body); err != nil {
		return fmt.Errorf("WaitForNextInvocation: can't read the body: %v", err)
	}
	defer response.Body.Close()

	var payload Payload
	if err := json.Unmarshal(body, &payload); err != nil {
		return fmt.Errorf("WaitForNextInvocation: can't unmarshal the payload: %v", err)
	}

	if payload.EventType == "SHUTDOWN" {
		if statsdServer != nil {
			// flush metrics synchronously
			statsdServer.Flush(true)
		}
		// shutdown the serverless agent
		stopCh <- struct{}{}
	}

	return nil
}
