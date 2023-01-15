package cloudfoundry

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"github.com/labstack/echo/v4"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/cloudfoundry-incubator/stratos/src/jetstream/repository/interfaces"
	"github.com/cloudfoundry/noaa/consumer"
	noaa_errors "github.com/cloudfoundry/noaa/errors"
	"github.com/cloudfoundry/sonde-go/events"
	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
)

const (
	// Time allowed to read the next pong message from the peer
	pongWait = 30 * time.Second

	// Send ping messages to peer with this period (must be less than pongWait)
	pingPeriod = (pongWait * 9) / 10
)

type logCacheResponse struct {
	Envelopes struct {
		Batch []logCacheMessage `json:"batch"`
	} `json:"envelopes"`
}

type logCacheMessage struct {
	Timestamp      string `json:"timestamp"`
	SourceID       string `json:"source_id"`
	InstanceID     string `json:"instance_id"`
	DeprecatedTags struct {
	} `json:"deprecated_tags"`
	Tags struct {
		AppID             string `json:"app_id"`
		AppName           string `json:"app_name"`
		Deployment        string `json:"deployment"`
		Index             string `json:"index"`
		InstanceID        string `json:"instance_id"`
		IP                string `json:"ip"`
		Job               string `json:"job"`
		OrganizationID    string `json:"organization_id"`
		OrganizationName  string `json:"organization_name"`
		Origin            string `json:"origin"`
		ProcessID         string `json:"process_id"`
		ProcessInstanceID string `json:"process_instance_id"`
		ProcessType       string `json:"process_type"`
		SourceID          string `json:"source_id"`
		SourceType        string `json:"source_type"`
		SpaceID           string `json:"space_id"`
		SpaceName         string `json:"space_name"`
	} `json:"tags"`
	Log struct {
		Payload string `json:"payload"`
		Type    string `json:"type"`
	} `json:"log"`
}

// Allow connections from any Origin
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func (c CloudFoundrySpecification) appStream(echoContext echo.Context) error {
	return c.commonStreamHandler(echoContext, appStreamHandler)
}

func (c CloudFoundrySpecification) firehose(echoContext echo.Context) error {
	return c.commonStreamHandler(echoContext, firehoseStreamHandler)
}

func (c CloudFoundrySpecification) appFirehose(echoContext echo.Context) error {
	return c.commonStreamHandler(echoContext, appFirehoseStreamHandler)
}

func (c CloudFoundrySpecification) commonStreamHandler(echoContext echo.Context, bespokeStreamHandler func(echo.Context, *AuthorizedConsumer, *websocket.Conn) error) error {
	ac, err := c.openNoaaConsumer(echoContext)
	if err != nil {
		return err
	}
	defer ac.consumer.Close()

	clientWebSocket, pingTicker, err := interfaces.UpgradeToWebSocket(echoContext)
	if err != nil {
		return err
	}
	defer clientWebSocket.Close()
	defer pingTicker.Stop()

	if err := bespokeStreamHandler(echoContext, ac, clientWebSocket); err != nil {
		return err
	}

	// This blocks until the WebSocket is closed
	drainClientMessages(clientWebSocket)
	return nil
}

type AuthorizedConsumer struct {
	consumer     *consumer.Consumer
	authToken    string
	refreshToken func() error
}

// Refresh the Authorization token if needed and create a new Noaa consumer
func (c CloudFoundrySpecification) openNoaaConsumer(echoContext echo.Context) (*AuthorizedConsumer, error) {

	ac := &AuthorizedConsumer{}

	// Get the CNSI and app IDs from route parameters
	cnsiGUID := echoContext.Param("cnsiGuid")
	userGUID := echoContext.Get("user_id").(string)

	// Extract the Doppler endpoint from the CNSI record
	cnsiRecord, err := c.portalProxy.GetCNSIRecord(cnsiGUID)
	if err != nil {
		return nil, fmt.Errorf("Failed to get record for CNSI %s: [%v]", cnsiGUID, err)
	}

	ac.refreshToken = func() error {
		newTokenRecord, err := c.portalProxy.RefreshOAuthToken(cnsiRecord.SkipSSLValidation, cnsiGUID, userGUID, cnsiRecord.ClientId, cnsiRecord.ClientSecret, cnsiRecord.TokenEndpoint)
		if err != nil {
			msg := fmt.Sprintf("Error refreshing token for CNSI %s : [%v]", cnsiGUID, err)
			return echo.NewHTTPError(http.StatusUnauthorized, msg)
		}
		ac.authToken = "bearer " + newTokenRecord.AuthToken
		return nil
	}

	dopplerAddress := cnsiRecord.DopplerLoggingEndpoint
	log.Debugf("CNSI record Obtained! Using Doppler Logging Endpoint: %s", dopplerAddress)

	// Get the auth token for the CNSI from the DB, refresh it if it's expired
	if tokenRecord, ok := c.portalProxy.GetCNSITokenRecord(cnsiGUID, userGUID); ok && !tokenRecord.Disconnected {
		ac.authToken = "bearer " + tokenRecord.AuthToken
		expTime := time.Unix(tokenRecord.TokenExpiry, 0)
		if expTime.Before(time.Now()) {
			log.Debug("Token obtained has expired, refreshing!")
			if err = ac.refreshToken(); err != nil {
				return nil, err
			}
		}
	} else {
		return nil, fmt.Errorf("Error getting token for user %s on CNSI %s", userGUID, cnsiGUID)
	}

	// Open a Noaa consumer to the doppler endpoint
	log.Debugf("Creating Noaa consumer for Doppler endpoint %s", dopplerAddress)
	ac.consumer = consumer.New(dopplerAddress, &tls.Config{InsecureSkipVerify: true}, http.ProxyFromEnvironment)

	return ac, nil
}

// Attempts to get the recent logs, if we get an unauthorized error we will refresh the auth token and retry once
func getRecentLogs(ac *AuthorizedConsumer, cnsiGUID, appGUID string) ([]*events.LogMessage, error) {
	log.Debug("getRecentLogs")

	// fetch logs from log-cache
	logCacheResponse, err := getLogCacheLogs(appGUID, ac.authToken)
	if err != nil {
		errorPattern := "New: failed to get recent messages for App %s on CNSI %s [%v]"
		if _, ok := err.(*noaa_errors.UnauthorizedError); ok {
			// If unauthorized, we may need to refresh our Auth token and try again
			if err := ac.refreshToken(); err != nil {
				return nil, fmt.Errorf(errorPattern, appGUID, cnsiGUID, err)
			}
			logCacheResponse, err = getLogCacheLogs(appGUID, ac.authToken)
			if err != nil {
				msg := fmt.Sprintf(errorPattern, appGUID, cnsiGUID, err)
				return nil, echo.NewHTTPError(http.StatusUnauthorized, msg)
			}
		} else {
			return nil, fmt.Errorf(errorPattern, appGUID, cnsiGUID, err)
		}
	}

	// transform log-cache format to legacy format
	messages := logCacheTranslation(&logCacheResponse)
	// messages, err := ac.consumer.RecentLogs(appGUID, ac.authToken)

	if err != nil {
		errorPattern := "Old: Failed to get recent messages for App %s on CNSI %s [%v]"
		if _, ok := err.(*noaa_errors.UnauthorizedError); ok {
			// If unauthorized, we may need to refresh our Auth token
			// Note: annoyingly, older versions of CF also send back "401 - Unauthorized" when the app doesn't exist...
			// This means we sometimes end up here even when our token is legit
			if err := ac.refreshToken(); err != nil {
				return nil, fmt.Errorf(errorPattern, appGUID, cnsiGUID, err)
			}
			messages, err = ac.consumer.RecentLogs(appGUID, ac.authToken)
			if err != nil {
				msg := fmt.Sprintf(errorPattern, appGUID, cnsiGUID, err)
				return nil, echo.NewHTTPError(http.StatusUnauthorized, msg)
			}
		} else {
			return nil, fmt.Errorf(errorPattern, appGUID, cnsiGUID, err)
		}
	}
	return messages, nil
}

func getLogCacheLogs(token string, appGuid string) (logCacheResponse, error) {
	envelopType := "LOG"
	logCacheEndpoint := os.Getenv("LOG_CACHE_ENDPOINT")
	logCacheUrl := logCacheEndpoint + appGuid + "?envelope_types=" + envelopType

	// ctx := context.Background()
	request, err := http.NewRequest(http.MethodGet, logCacheUrl, nil)
	if err != nil {
		log.Fatalln("Could not generate HTTP request")
	}
	request.Header.Set("Authorization", token)
	client := &http.Client{}
	response, err := client.Do(request)

	defer response.Body.Close()

	if response.StatusCode == http.StatusOK {
		bytes, _ := io.ReadAll(response.Body)
		var logCacheResponse logCacheResponse // The new log cache format

		// Debug Info
		// responseText := string(bytes)
		// fmt.Println(responseText)

		json.Unmarshal(bytes, &logCacheResponse)
		return logCacheResponse, nil
	}
	// Empty response if status not OK
	return logCacheResponse{}, err
}

// hotfix for loggregator 107.0.0 breaking change ( removal of deprecated RecentLogs )
func logCacheTranslation(res *logCacheResponse) []*events.LogMessage {
	var logMessages []*events.LogMessage
	for _, m := range res.Envelopes.Batch {

		// conversion from string to int64 because events.LogMessage wants that
		i, err := strconv.ParseInt(m.Timestamp, 10, 64)
		if err != nil {
			log.Println("unable to transform string unix timestamp to int64")
		}
		// conversion of message Type
		var logType events.LogMessage_MessageType

		msg := events.LogMessage{
			Timestamp:      &i,
			AppId:          &m.SourceID,
			MessageType:    logType.Enum(),
			SourceType:     &m.Tags.SourceType,
			Message:        []byte(m.Log.Payload),
			SourceInstance: &m.InstanceID,
		}

		logMessages = append(logMessages, &msg)
	}
	return logMessages
}

func drainErrors(errorChan <-chan error) {
	for err := range errorChan {
		// Note: we receive a nil error before the channel is closed so check here...
		if err != nil {
			log.Errorf("Received error from Doppler %v", err.Error())
		}
	}
}

func drainLogMessages(msgChan <-chan *events.LogMessage, callback func(msg *events.LogMessage)) {
	for msg := range msgChan {
		callback(msg)
	}
}

func drainFirehoseEvents(eventChan <-chan *events.Envelope, callback func(msg *events.Envelope)) {
	for event := range eventChan {
		callback(event)
	}
}

// Drain and discard incoming messages from the WebSocket client, effectively making our WebSocket read-only
func drainClientMessages(clientWebSocket *websocket.Conn) {
	for {
		_, _, err := clientWebSocket.ReadMessage()
		if err != nil {
			// We get here when the client (browser) disconnects
			break
		}
	}
}

func appStreamHandler(echoContext echo.Context, ac *AuthorizedConsumer, clientWebSocket *websocket.Conn) error {
	// Get the CNSI and app IDs from route parameters
	cnsiGUID := echoContext.Param("cnsiGuid")
	appGUID := echoContext.Param("appGuid")

	log.Infof("Received request for log stream for App ID: %s - in CNSI: %s", appGUID, cnsiGUID)

	messages, err := getRecentLogs(ac, cnsiGUID, appGUID)
	if err != nil {
		return err
	}
	// Reusable closure to pump messages from Noaa to the client WebSocket
	// N.B. We convert protobuf messages to JSON for ease of use in the frontend
	relayLogMsg := func(msg *events.LogMessage) {
		if jsonMsg, err := json.Marshal(msg); err != nil {
			log.Errorf("Received unparsable message from Doppler %v, %v", jsonMsg, err)
		} else {
			err := clientWebSocket.WriteMessage(websocket.TextMessage, jsonMsg)
			if err != nil {
				log.Errorf("Error writing data to WebSocket, %v", err)
			}
		}
	}

	// Send the recent messages, sorted in Chronological order
	//for _, msg := range noaa.SortRecent(messages) {
	// log-cache sends sorted output.
	for _, msg := range messages {
		relayLogMsg(msg)
	}

	msgChan, errorChan := ac.consumer.TailingLogs(appGUID, ac.authToken)

	// Process the app stream
	go drainErrors(errorChan)
	go drainLogMessages(msgChan, relayLogMsg)

	log.Infof("Now streaming log for App ID: %s - on CNSI: %s", appGUID, cnsiGUID)
	return nil
}

func firehoseStreamHandler(echoContext echo.Context, ac *AuthorizedConsumer, clientWebSocket *websocket.Conn) error {
	log.Debug("firehose")

	// Get the CNSI and app IDs from route parameters
	cnsiGUID := echoContext.Param("cnsiGuid")

	log.Infof("Received request for Firehose stream for CNSI: %s", cnsiGUID)

	userGUID := echoContext.Get("user_id").(string)
	firehoseSubscriptionId := userGUID + "@" + strconv.FormatInt(time.Now().UnixNano(), 10)
	log.Debugf("Connecting the Firehose with subscription ID: %s", firehoseSubscriptionId)

	eventChan, errorChan := ac.consumer.Firehose(firehoseSubscriptionId, ac.authToken)

	// Process the app stream
	go drainErrors(errorChan)
	go drainFirehoseEvents(eventChan, func(msg *events.Envelope) {
		if jsonMsg, err := json.Marshal(msg); err != nil {
			log.Errorf("Received unparsable message from Doppler %v, %v", jsonMsg, err)
		} else {
			err := clientWebSocket.WriteMessage(websocket.TextMessage, jsonMsg)
			if err != nil {
				log.Errorf("Error writing data to WebSocket, %v", err)
			}
		}
	})

	log.Infof("Firehose connected and streaming for CNSI: %s - subscription ID: %s", cnsiGUID, firehoseSubscriptionId)
	return nil
}

func appFirehoseStreamHandler(echoContext echo.Context, ac *AuthorizedConsumer, clientWebSocket *websocket.Conn) error {
	log.Debug("appFirehoseStreamHandler")

	// Get the CNSI and app IDs from route parameters
	cnsiGUID := echoContext.Param("cnsiGuid")
	appGUID := echoContext.Param("appGuid")

	log.Infof("Received request for log stream for App ID: %s - in CNSI: %s", appGUID, cnsiGUID)

	msgChan, errorChan := ac.consumer.Stream(appGUID, ac.authToken)

	// Process the app stream
	go drainErrors(errorChan)
	go drainFirehoseEvents(msgChan, func(msg *events.Envelope) {
		if jsonMsg, err := json.Marshal(msg); err != nil {
			log.Errorf("Received unparsable message from Doppler %v, %v", jsonMsg, err)
		} else {
			err := clientWebSocket.WriteMessage(websocket.TextMessage, jsonMsg)
			if err != nil {
				log.Errorf("Error writing data to WebSocket, %v", err)
			}
		}
	})

	log.Infof("Now streaming for App ID: %s - on CNSI: %s", appGUID, cnsiGUID)
	return nil
}
