package model

// Context is the evaluation context an application passes with a flag check.
type Context struct {
	UserID     string
	Attributes map[string]any
}

// Reason explains why an evaluation produced its value. The values are shared by every SDK.
type Reason string

// Evaluation reasons, aligned with OpenFeature.
const (
	ReasonDisabled       Reason = "DISABLED"
	ReasonStatic         Reason = "STATIC"
	ReasonTargetingMatch Reason = "TARGETING_MATCH"
	ReasonSplit          Reason = "SPLIT"
	ReasonDefault        Reason = "DEFAULT"
	ReasonError          Reason = "ERROR"
)

// ErrorCode qualifies a Result whose Reason is ReasonError. It is empty otherwise.
type ErrorCode string

// Evaluation error codes, aligned with OpenFeature.
const (
	ErrorCodeFlagNotFound ErrorCode = "FLAG_NOT_FOUND"
	ErrorCodeParseError   ErrorCode = "PARSE_ERROR"
	ErrorCodeGeneral      ErrorCode = "GENERAL"
)

// Result is the outcome of one evaluation.
type Result struct {
	Value       bool
	Reason      Reason
	FlagVersion int64
	ErrorCode   ErrorCode
}
