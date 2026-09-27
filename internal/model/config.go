package model

// FlagConfig is the evaluation configuration of a flag in one environment. It is designed for
// growth: rules and percentage splits have a place here even though the v0.1 engine supports only
// an empty rule list and a fixed fallthrough value.
type FlagConfig struct {
	Rules       []Rule
	Fallthrough Serve // served when no rule matches; zero value means "serve true"
	// Unparseable marks a configuration that could not be decoded (for example one written by a
	// newer version). Such a flag evaluates as PARSE_ERROR and cannot be saved.
	Unparseable bool
}

// Rule serves Serve when every condition matches the evaluation context.
type Rule struct {
	Conditions []Condition
	Serve      Serve
}

// Condition compares one context attribute with a list of values using Operator.
type Condition struct {
	Attribute string
	Operator  string
	Values    []string
}

// Serve is what a rule or the fallthrough returns: a fixed Value or a percentage Split. Setting
// both is invalid.
type Serve struct {
	Value *bool
	Split *Split
}

// Split assigns a context to a variation by deterministic bucketing on BucketBy
// ("user_id" or "attr:<name>"). Weights are in units of 0.001% and sum to 100000.
type Split struct {
	Variations []WeightedVariation
	BucketBy   string
	Salt       string
}

// WeightedVariation is one outcome of a Split.
type WeightedVariation struct {
	Value  bool
	Weight int
}
