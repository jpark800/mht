package triggerhttpnew

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/TIBCOSoftware/flogo-contrib/trigger/rest/cors"
	"github.com/TIBCOSoftware/flogo-lib/core/action"
	"github.com/TIBCOSoftware/flogo-lib/core/trigger"
	"github.com/TIBCOSoftware/flogo-lib/logger"
	condition "github.com/TIBCOSoftware/mashling-lib/conditions"
	"github.com/TIBCOSoftware/mashling-lib/util"
	"github.com/julienschmidt/httprouter"
)

const (
	REST_CORS_PREFIX = "REST_TRIGGER"
)

// log is the default package logger
var log = logger.GetLogger("trigger-tibco-rest")

//OptimizedHandler optimized handler
type OptimizedHandler struct {
	defaultActionId string
	settings        map[string]interface{}
	dispatches      []*Dispatch
}

//Dispatch holds dispatch actionId and condition
type Dispatch struct {
	actionId  string
	condition string
}

var validMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"}

// RestTrigger REST trigger struct
type RestTrigger struct {
	metadata *trigger.Metadata
	runner   action.Runner
	server   *Server
	config   *trigger.Config
}

//NewFactory create a new Trigger factory
func NewFactory(md *trigger.Metadata) trigger.Factory {
	return &RestFactory{metadata: md}
}

// RestFactory REST Trigger factory
type RestFactory struct {
	metadata *trigger.Metadata
}

//New Creates a new trigger instance for a given id
func (t *RestFactory) New(config *trigger.Config) trigger.Trigger {
	return &RestTrigger{metadata: t.metadata, config: config}
}

// Metadata implements trigger.Trigger.Metadata
func (t *RestTrigger) Metadata() *trigger.Metadata {
	return t.metadata
}

//Init trigger initialization
func (t *RestTrigger) Init(runner action.Runner) {

	log.SetLogLevel(logger.DebugLevel)

	router := httprouter.New()

	if t.config.Settings == nil {
		panic(fmt.Sprintf("No Settings found for trigger '%s'", t.config.Id))
	}

	if _, ok := t.config.Settings["port"]; !ok {
		panic(fmt.Sprintf("No Port found for trigger '%s' in settings", t.config.Id))
	}

	addr := ":" + t.config.GetSetting("port")
	t.runner = runner

	//optimize flog-handlers i.e merge handlers having same settings
	optHandlers := []*OptimizedHandler{}
	for _, handler := range t.config.Handlers {
		//check if there is any handler already added with same settings
		handlerAdded := false
		for _, optHandler := range optHandlers {
			//loop through all settings
			settingsMatched := true
			for k, v := range optHandler.settings {
				if v != handler.Settings[k] {
					settingsMatched = false
					break
				}
			}
			if settingsMatched {
				//check for dispatch condition
				if dispatchCondition := handler.Settings[util.Flogo_Trigger_Handler_Setting_Condition]; dispatchCondition != nil {
					tmpDispatch := &Dispatch{
						actionId:  handler.ActionId,
						condition: dispatchCondition.(string),
					}
					optHandler.dispatches = append(optHandler.dispatches, tmpDispatch)
				} else {
					//no dispatch condition, hence make it as default action
					optHandler.defaultActionId = handler.ActionId
				}
				handlerAdded = true
				break
			}
		}

		if !handlerAdded {
			tmpSettings := make(map[string]interface{})
			for k, v := range handler.Settings {
				if k != util.Flogo_Trigger_Handler_Setting_Condition {
					tmpSettings[k] = v
				}
			}

			var tmpDispatches []*Dispatch
			//check for dispatch condition
			if dispatchCondition := handler.Settings[util.Flogo_Trigger_Handler_Setting_Condition]; dispatchCondition != nil {
				tmpDispatch := &Dispatch{
					actionId:  handler.ActionId,
					condition: handler.Settings[util.Flogo_Trigger_Handler_Setting_Condition].(string),
				}
				tmpDispatches = append(tmpDispatches, tmpDispatch)
			}

			optHandler := OptimizedHandler{
				defaultActionId: handler.ActionId,
				settings:        tmpSettings,
				dispatches:      tmpDispatches,
			}

			optHandlers = append(optHandlers, &optHandler)
		}
	}

	// Init handlers
	for _, optHandler := range optHandlers {
		if handlerIsValid(optHandler) {
			method := strings.ToUpper(optHandler.settings["method"].(string))
			path := optHandler.settings["path"].(string)
			log.Debugf("REST Trigger: Registering handler [%s: %s] with default Action Id: [%s]", method, path, optHandler.defaultActionId)
			router.OPTIONS(path, handleCorsPreflight)
			router.Handle(method, path, newActionHandler(t, optHandler))
		}
	}

	log.Debugf("REST Trigger: Configured on port %s", t.config.Settings["port"])
	t.server = NewServer(addr, router)
}

func (t *RestTrigger) Start() error {
	return t.server.Start()
}

// Stop implements util.Managed.Stop
func (t *RestTrigger) Stop() error {
	return t.server.Stop()
}

// Handles the cors preflight request
func handleCorsPreflight(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {

	log.Infof("Received [OPTIONS] request to CorsPreFlight: %+v", r)

	c := cors.New(REST_CORS_PREFIX, log)
	c.HandlePreflight(w, r)
}

// IDResponse id response object
type IDResponse struct {
	ID string `json:"id"`
}

func newActionHandler(rt *RestTrigger, handler *OptimizedHandler) httprouter.Handle {

	return func(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {

		log.Infof("REST Trigger: Received request for id '%s'", rt.config.Id)

		c := cors.New(REST_CORS_PREFIX, log)
		c.WriteCorsActualRequestHeaders(w)

		pathParams := make(map[string]string)
		for _, param := range ps {
			pathParams[param.Key] = param.Value
		}

		var content interface{}
		err := json.NewDecoder(r.Body).Decode(&content)
		if err != nil {
			switch {
			case err == io.EOF:
			// empty body
			//todo should handler say if content is expected?
			case err != nil:
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}

		queryValues := r.URL.Query()
		queryParams := make(map[string]string, len(queryValues))

		for key, value := range queryValues {
			queryParams[key] = strings.Join(value, ",")
		}

		data := map[string]interface{}{
			"params":      pathParams,
			"pathParams":  pathParams,
			"queryParams": queryParams,
			"content":     content,
		}

		//pick action based on dispatch condition
		contentBytes, err := json.Marshal(content)
		contentStr := string(contentBytes)
		actionId := ""

		for _, dispatch := range handler.dispatches {
			expressionStr := dispatch.condition
			conditionOperation, err := condition.GetConditionOperation(expressionStr)
			if err != nil {
				log.Errorf("not able parse the condition '%v' mentioned for content based handler. skipping the handler.", expressionStr)
				continue
			}
			//evaluate expression
			exprResult, err := condition.EvaluateCondition(*conditionOperation, contentStr)
			if err != nil {
				log.Errorf("not able evaluate expression - %v with error - %v. skipping the handler.", expressionStr, err)
			}
			if exprResult {
				actionId = dispatch.actionId
				log.Debugf("dispatch resolved with the actionId - %v", actionId)
				break
			}
		}
		//If no dispatch is found, use default action
		if actionId == "" {
			actionId = handler.defaultActionId
			log.Debugf("dispatch not resolved. Continue with default action - %v", actionId)
		}

		//todo handle error
		startAttrs, _ := rt.metadata.OutputsToAttrs(data, false)

		action := action.Get(actionId)
		log.Debugf("Found action' %+x'", action)

		context := trigger.NewContext(context.Background(), startAttrs)
		replyCode, replyData, err := rt.runner.Run(context, action, actionId, nil)

		if err != nil {
			log.Debugf("REST Trigger Error: %s", err.Error())
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if replyData != nil {
			w.Header().Set("Content-Type", "application/json; charset=UTF-8")
			w.WriteHeader(replyCode)
			if err := json.NewEncoder(w).Encode(replyData); err != nil {
				log.Error(err)
			}
		}

		if replyCode > 0 {
			w.WriteHeader(replyCode)
		} else {
			w.WriteHeader(http.StatusOK)
		}
	}
}

////////////////////////////////////////////////////////////////////////////////////////
// Utils
func handlerIsValid(handler *OptimizedHandler) bool {
	if handler.settings == nil {
		return false
	}

	if handler.settings["method"] == "" {
		return false
	}

	if !stringInList(strings.ToUpper(handler.settings["method"].(string)), validMethods) {
		return false
	}

	//validate path

	return true
}

func stringInList(str string, list []string) bool {
	for _, value := range list {
		if value == str {
			return true
		}
	}
	return false
}
