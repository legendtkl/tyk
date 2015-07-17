package main

import (
	"strconv"
	"strings"
	"time"
)

type HealthPrefix string

const (
	Throttle          HealthPrefix = "Throttle"
	QuotaViolation    HealthPrefix = "QuotaViolation"
	KeyFailure        HealthPrefix = "KeyFailure"
	RequestLog        HealthPrefix = "Request"
	BlockedRequestLog HealthPrefix = "BlockedRequest"

	HealthCheckRedisPrefix string = "apihealth"
)

type HealthChecker interface {
	Init(StorageHandler)
	GetApiHealthValues() (HealthCheckValues, error)
	StoreCounterVal(HealthPrefix, string)
}

type HealthCheckValues struct {
	ThrottledRequestsPS float64 `bson:"throttle_reqests_per_second,omitempty" json:"throttle_reqests_per_second"`
	QuotaViolationsPS   float64 `bson:"quota_violations_per_second,omitempty" json:"quota_violations_per_second"`
	KeyFailuresPS       float64 `bson:"key_failures_per_second,omitempty" json:"key_failures_per_second"`
	AvgUpstreamLatency  float64 `bson:"average_upstream_latency,omitempty" json:"average_upstream_latency"`
	AvgRequestsPS       float64 `bson:"average_requests_per_second,omitempty" json:"average_requests_per_second"`
}

type DefaultHealthChecker struct {
	storage StorageHandler
	APIID   string
}

func (h *DefaultHealthChecker) Init(storeType StorageHandler) {
	if config.HealthCheck.EnableHealthChecks {
		log.Debug("Health Checker initialised.")
	}

	h.storage = storeType
	h.storage.Connect()
}

func (h *DefaultHealthChecker) CreateKeyName(subKey HealthPrefix) string {
	var newKey string
	now := time.Now().UnixNano()

	// Key should be API-ID.SubKey.123456789
	newKey = strings.Join([]string{h.APIID, string(subKey), strconv.FormatInt(now, 10)}, ".")

	return newKey
}

// ReportHealthCheckValue is a shortcut we can use throughout the app to push a health check value
func ReportHealthCheckValue(checker HealthChecker, counter HealthPrefix, value string) {
	// TODO: Wrap this in a conditional so it can be deactivated
	go checker.StoreCounterVal(counter, value)
}

func (h *DefaultHealthChecker) StoreCounterVal(counterType HealthPrefix, value string) {
	if config.HealthCheck.EnableHealthChecks {
		searchStr := h.CreateKeyName(counterType)
		log.Debug("Adding Healthcheck to: ", searchStr)
		go h.storage.SetKey(searchStr, value, config.HealthCheck.HealthCheckValueTimeout)
	}
}

func (h *DefaultHealthChecker) getAvgCount(prefix HealthPrefix) float64 {
	searchStr := strings.Join([]string{h.APIID, string(prefix)}, ".")
	log.Debug("Searching for: ", searchStr)
	keys := h.storage.GetKeys(searchStr)
	log.Debug("Found ", keys)
	var count int64
	count = int64(len(keys))
	divisor := float64(config.HealthCheck.HealthCheckValueTimeout)
	if divisor == 0 {
		log.Warning("The Health Check sample timeout is set to 0, samples will never be deleted!!!")
		divisor = 60.0
	}
	if count > 0 {
		return roundValue(float64(count) / divisor)
	}

	return 0.00
}

func roundValue(untruncated float64) float64 {
	truncated := float64(int(untruncated*100)) / 100

	return truncated
}

func (h *DefaultHealthChecker) GetApiHealthValues() (HealthCheckValues, error) {
	values := HealthCheckValues{}

	// Get the counted / average values
	values.ThrottledRequestsPS = h.getAvgCount(Throttle)
	values.QuotaViolationsPS = h.getAvgCount(QuotaViolation)
	values.KeyFailuresPS = h.getAvgCount(KeyFailure)
	values.AvgRequestsPS = h.getAvgCount(RequestLog)

	// Get the micro latency graph, an average upstream latency
	searchStr := strings.Join([]string{h.APIID, string(RequestLog)}, ".")
	log.Debug("Searching KV for: ", searchStr)
	kv := h.storage.GetKeysAndValuesWithFilter(searchStr)
	log.Debug("Found: ", kv)
	var runningTotal int
	if len(kv) > 0 {
		for _, v := range kv {
			vInt, cErr := strconv.Atoi(v)
			if cErr != nil {
				log.Error("Couldn't convert tracked latency value to Int, vl is: ")
			} else {
				runningTotal += vInt
			}
		}
		values.AvgUpstreamLatency = roundValue(float64(runningTotal / len(kv)))
	}

	return values, nil
}
