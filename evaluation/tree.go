package evaluation

import (
	"errors"
	"fmt"
	"github.com/flukeish/pmml/models"
	"strconv"
)

type TreeModel struct {
	validated bool
	compiled  bool

	dd *models.DataDictionary
	td *models.TransformationDictionary

	model *models.TreeModel

	fieldTypes map[models.FieldName]models.DataType
	root       node

	outKeys map[string]string
	targetField models.FieldName
}

type test func(DataRow) predicateResult

type scoreDist struct {
	value       string
	confidence  float64
	probability float64
	recordCount float64
}

type node struct {
	id           string
	defaultChild string
	children     []node

	scoreDist []scoreDist

	test  test
	score scoreDist
	m     *TreeModel
}

func (n node) leaf() bool {
	return len(n.children) == 0
}

type predicateResult int

const (
	u predicateResult = iota
	t
	f
)

/*
const (
	MissingValueStrategyAggregateNodes     = MissingValueStrategy("aggregateNodes")
	MissingValueStrategyDefaultChild       = MissingValueStrategy("defaultChild")
	MissingValueStrategyLastPrediction     = MissingValueStrategy("lastPrediction")
	MissingValueStrategyNone               = MissingValueStrategy("none")
	MissingValueStrategyNullPrediction     = MissingValueStrategy("nullPrediction")
	MissingValueStrategyWeightedConfidence = MissingValueStrategy("weightedConfidence")
)
*/

func pickBest(scores []scoreDist) scoreDist {
	best := scoreDist{probability: 0.0}

	for _, score := range scores {
		if score.probability > best.probability {
			best = score
		}
	}

	return best
}

func (n node) evaluate(input DataRow) ([]scoreDist, predicateResult) {
	switch n.m.model.MissingValueStrategy {
	case models.MissingValueStrategyNullPrediction:
		return n.evaluateMissingValueStrategyNullPrediction(input)
	case models.MissingValueStrategyAggregateNodes:
		return n.evaluateMissingValueStrategyAggregateNodes(input)
	case models.MissingValueStrategyWeightedConfidence:
		return n.evaluateMissingValueStrategyWeightedConfidence(input)
	case models.MissingValueStrategyDefaultChild:
		return n.evaluateMissingValueStrategyDefaultChild(input)
	case models.MissingValueStrategyLastPrediction:
		return n.evaluateMissingValueStrategyLastPrediction(input)
	default:
		return n.evaluateMissingValueStrategyNone(input)
	}
}

var emptyScoreDist []scoreDist

func (n node) outputScoreDist() []scoreDist {
	if !n.leaf() && n.m.model.NoTrueChildStrategy == models.NoTrueChildStrategyReturnNullPrediction {
		return nil
	}

	if len(n.scoreDist) > 0 {
		return n.scoreDist
	}

	sd := make([]scoreDist, 0, 0)

	if n.m.targetField != "" {
		for _, field := range n.m.dd.DataFields {
			if field.Name == n.m.targetField {
				for _, val := range field.Values {
					if val.Value == n.score.value {
						sd = append(sd, scoreDist{val.Value, 1.0, 1.0, n.score.recordCount})
					} else {
						sd = append(sd, scoreDist{val.Value, 0.0, 0.0, 0})
					}
				}
			}
		}
	} else {
		sd = append(sd, n.score)
	}

	return sd
}

func (n node) evaluateMissingValueStrategyNone(input DataRow) ([]scoreDist, predicateResult) {
	result := n.test(input)

	if result == f || result == u {
		return emptyScoreDist, f
	}

	for _, c := range n.children {
		score, childResult := c.evaluateMissingValueStrategyNone(input)

		if childResult == t {
			return score, childResult
		}
	}

	return n.outputScoreDist(), result
}

func (n node) evaluateMissingValueStrategyLastPrediction(input DataRow) ([]scoreDist, predicateResult) {
	result := n.test(input)

	if result == f {
		return emptyScoreDist, result
	}

	unknownValues := false
	var trueDist []scoreDist

	for _, c := range n.children {
		score, childResult := c.evaluateMissingValueStrategyLastPrediction(input)

		if childResult == u {
			unknownValues = true
		}

		if childResult == t {
			trueDist = score
		}
	}

	if !unknownValues && len(trueDist) != 0 {
		return trueDist, t
	}

	return n.outputScoreDist(), result
}

func (n node) evaluateMissingValueStrategyDefaultChild(input DataRow) ([]scoreDist, predicateResult) {
	result := n.test(input)

	if result == f {
		return emptyScoreDist, result
	}

	childScores := make(map[string][]scoreDist)
	unknownValues := false
	var trueDist []scoreDist

	for _, c := range n.children {
		score, childResult := c.evaluateMissingValueStrategyDefaultChild(input)
		childScores[c.id] = score

		if childResult == u {
			unknownValues = true
		}

		if childResult == t {
			trueDist = score
		}
	}

	if !unknownValues && len(trueDist) != 0 {
		return trueDist, t
	}

	if unknownValues {
		defaultScores, ok := childScores[n.defaultChild]
		if !ok {
			return emptyScoreDist, u
		}

		adjustedScores := make([]scoreDist, len(defaultScores), len(defaultScores))

		for idx, score := range defaultScores {
			score.confidence = score.confidence * float64(n.m.model.MissingValuePenalty)
			score.probability = score.probability * float64(n.m.model.MissingValuePenalty)
			adjustedScores[idx] = score
		}

		return adjustedScores, t
	}

	return n.outputScoreDist(), result
}

func (n node) evaluateMissingValueStrategyNullPrediction(input DataRow) ([]scoreDist, predicateResult) {
	result := n.test(input)

	if result == f {
		return emptyScoreDist, result
	}

	if result == u {
		return emptyScoreDist, result
	}

	for _, c := range n.children {
		score, childResult := c.evaluateMissingValueStrategyNullPrediction(input)

		if childResult == t {
			return score, childResult
		}

		if childResult == u {
			return emptyScoreDist, childResult
		}
	}

	return n.outputScoreDist(), result
}

func (n node) evaluateMissingValueStrategyWeightedConfidence(input DataRow) ([]scoreDist, predicateResult) {
	result := n.test(input)

	if result == f {
		return n.scoreDist, result
	}

	unknownValues := false
	var trueDist []scoreDist
	childRecordCounts := make(map[string]float64)
	childScores := make(map[string][]scoreDist)
	totalRecordCount := 0.0

	for _, c := range n.children {
		scores, childResult := c.evaluateMissingValueStrategyWeightedConfidence(input)

		if childResult != f {
			childRecordCounts[c.id] = c.score.recordCount
			childScores[c.id] = scores
			totalRecordCount += c.score.recordCount
		}

		if childResult == u {
			unknownValues = true
		} else if childResult == t {
			trueDist = scores
		}
	}

	if !unknownValues && len(trueDist) > 0 {
		return trueDist, result
	}

	if unknownValues {
		aggScoreMap := make(map[string]scoreDist)

		for nid, scores := range childScores {
			for _, score := range scores {
				aggScore, ok := aggScoreMap[score.value]
				if !ok {
					aggScore = scoreDist{value: score.value}
				}
				nodeRecordCount := childRecordCounts[nid]

				aggScore.recordCount += score.recordCount
				aggScore.probability += score.probability * (nodeRecordCount / totalRecordCount)
				aggScore.confidence += score.confidence * (nodeRecordCount / totalRecordCount)

				aggScoreMap[score.value] = aggScore
			}
		}

		aggScores := make([]scoreDist, 0, 0)
		for _, aggScore := range aggScoreMap {
			aggScores = append(aggScores, aggScore)
		}

		return aggScores, t
	}

	return n.outputScoreDist(), result
}

func (n node) evaluateMissingValueStrategyAggregateNodes(input DataRow) ([]scoreDist, predicateResult) {
	result := n.test(input)

	if result == f {
		return n.outputScoreDist(), result
	}

	childScores := make([]scoreDist, 0, 0)
	unknownValues := false
	var trueDist []scoreDist

	for _, c := range n.children {
		score, childResult := c.evaluateMissingValueStrategyAggregateNodes(input)
		childScores = append(childScores, score...)

		if childResult == u {
			unknownValues = true
		} else if childResult == t {
			trueDist = score
		}
	}

	if !unknownValues && len(trueDist) != 0 {
		return trueDist, result
	}

	if unknownValues {
		aggScoreMap := make(map[string]scoreDist)

		recordCount := 0.0
		for _, sc := range childScores {
			aggScore, ok := aggScoreMap[sc.value]

			if !ok {
				aggScore.value = sc.value
			}

			aggScore.recordCount += sc.recordCount
			aggScoreMap[sc.value] = aggScore
			recordCount += sc.recordCount
		}

		aggScores := make([]scoreDist, 0, 0)
		for _, aggScore := range aggScoreMap {
			aggScore.probability = aggScore.recordCount / recordCount
			aggScore.confidence = aggScore.recordCount / recordCount
			aggScores = append(aggScores, aggScore)
		}

		return aggScores, t
	}

	return n.outputScoreDist(), result
}

func NewTreeModel(dd *models.DataDictionary, td *models.TransformationDictionary, model *models.TreeModel) (*TreeModel, error) {
	m := new(TreeModel)
	m.dd = dd
	m.td = td
	m.model = model
	m.outKeys = make(map[string]string)

	err := m.Validate()
	if err != nil {
		return nil, err
	}

	err = m.Compile()
	if err != nil {
		return nil, err
	}

	return m, nil
}

func (m *TreeModel) Validate() error {
	if m.model.FunctionName != models.MiningFunctionClassification &&
		m.model.FunctionName != models.MiningFunctionRegression {
		return fmt.Errorf("unsupported mining function %s", m.model.FunctionName)
	}

	for i, f := range m.model.MiningSchema.MiningFields {
		if f.UsageType == "" {
			f.UsageType = models.FieldUsageTypeActive
		}

		if f.Outliers == "" {
			f.Outliers = models.OutlierTreatmentMethodAsIs
		}

		if f.InvalidValueTreatment == "" {
			f.InvalidValueTreatment = models.InvalidValueTreatmentMethodReturnInvalid
		}

		m.model.MiningSchema.MiningFields[i] = f
	}

	m.validated = true
	return nil
}

func newNode(m *TreeModel, modelNode models.Node) node {
	n := node{
		id:           modelNode.ID,
		defaultChild: modelNode.DefaultChild,
		m:            m,
	}

	score := scoreDist{
		value:       modelNode.Score,
		recordCount: float64(modelNode.RecordCount),
	}

	totalRecords := float64(0.0)

	for _, sc := range modelNode.ScoreDistributions {
		totalRecords += float64(sc.RecordCount)
	}

	for _, sc := range modelNode.ScoreDistributions {
		confidence := float64(sc.RecordCount) / totalRecords
		if sc.Confidence != nil {
			confidence = float64(*sc.Confidence)
		}

		probability := float64(sc.RecordCount) / totalRecords
		if sc.Probability != nil {
			probability = float64(*sc.Probability)
		}

		n.scoreDist = append(n.scoreDist, scoreDist{
			confidence:  confidence,
			probability: probability,
			recordCount: float64(sc.RecordCount),
			value:       sc.Value,
		})
	}

	n.score = score

	for _, child := range modelNode.Nodes {
		n.children = append(n.children, newNode(m, child))
	}

	switch p := modelNode.Predicate.(type) {
	case *models.True:
		n.test = func(DataRow) predicateResult { return t }
	case *models.False:
		n.test = func(DataRow) predicateResult { return f }
	case *models.SimplePredicate:
		n.test = func(input DataRow) predicateResult {
			return evaluateSimplePredicate(p, input, m.fieldTypes)
		}
	case *models.CompoundPredicate:
		n.test = func(input DataRow) predicateResult {
			return evaluateCompoundPredicate(p, input, m.fieldTypes)
		}
	case *models.SimpleSetPredicate:
		n.test = func(input DataRow) predicateResult {
			return f
		}
	}

	return n
}

func getRawValue(dt models.DataType, val string) (interface{}, error) {
	switch dt {
	case models.DataTypeBoolean:
		return strconv.ParseBool(val)
	case models.DataTypeDouble:
		return strconv.ParseFloat(val, 64)
	case models.DataTypeFloat:
		return strconv.ParseFloat(val, 64)
	case models.DataTypeInteger:
		return strconv.ParseInt(val, 10, 64)
	case models.DataTypeString:
		return val, nil
	}

	return nil, errors.New("invalid data type")
}

//nolint:gocyclo
func evaluateSimplePredicate(p *models.SimplePredicate, input DataRow, fieldTypes map[models.FieldName]models.DataType) predicateResult {
	predicateValueType, ok := fieldTypes[p.Field]
	if !ok {
		return f
	}

	if p.RawPredicateValue == nil {
		rawPredicateValue, err := getRawValue(predicateValueType, p.Value)
		if err != nil {
			return f
		}

		p.RawPredicateValue = rawPredicateValue
	}

	val, ok := input[string(p.Field)]
	if !ok {
		if p.Operator == models.SimplePredicateOperatorIsMissing {
			return t
		}

		return u
	}

	switch p.Operator {
	case models.SimplePredicateOperatorIsNotMissing:
		return t
	case models.SimplePredicateOperatorEqual:
		if val.Raw() == p.RawPredicateValue {
			return t
		}
	case models.SimplePredicateOperatorNotEqual:
		if val.Raw() != p.RawPredicateValue {
			return t
		}
	case models.SimplePredicateOperatorGreaterOrEqual:
		switch predicateValueType {
		case models.DataTypeDouble:
			if val.Float64() >= p.RawPredicateValue.(float64) {
				return t
			}
		case models.DataTypeFloat:
			if val.Float64() >= p.RawPredicateValue.(float64) {
				return t
			}
		case models.DataTypeInteger:
			if val.Int64() >= p.RawPredicateValue.(int64) {
				return t
			}
		}
	case models.SimplePredicateOperatorGreaterThan:
		switch predicateValueType {
		case models.DataTypeDouble:
			if val.Float64() > p.RawPredicateValue.(float64) {
				return t
			}
		case models.DataTypeFloat:
			if val.Float64() > p.RawPredicateValue.(float64) {
				return t
			}
		case models.DataTypeInteger:
			if val.Int64() > p.RawPredicateValue.(int64) {
				return t
			}
		}
	case models.SimplePredicateOperatorLessOrEqual:
		switch predicateValueType {
		case models.DataTypeDouble:
			if val.Float64() <= p.RawPredicateValue.(float64) {
				return t
			}
		case models.DataTypeFloat:
			if val.Float64() <= p.RawPredicateValue.(float64) {
				return t
			}
		case models.DataTypeInteger:
			if val.Int64() <= p.RawPredicateValue.(int64) {
				return t
			}
		}
	case models.SimplePredicateOperatorLessThan:
		switch predicateValueType {
		case models.DataTypeDouble:
			if val.Float64() < p.RawPredicateValue.(float64) {
				return t
			}
		case models.DataTypeFloat:
			if val.Float64() < p.RawPredicateValue.(float64) {
				return t
			}
		case models.DataTypeInteger:
			if val.Int64() < p.RawPredicateValue.(int64) {
				return t
			}
		}
	}

	return f
}

func evaluateSimpleSetPredicate(p *models.SimpleSetPredicate, input DataRow, fieldTypes map[models.FieldName]models.DataType) predicateResult {
	return f
}

func evaluateCompoundPredicate(p *models.CompoundPredicate, input DataRow, fieldTypes map[models.FieldName]models.DataType) predicateResult {
	trueCount := 0
	unknownCount := 0

	surrogate := p.BooleanOperator == models.CompoundPredicateOperatorSurrogate

	for _, child := range p.Predicates {
		var val predicateResult
		switch c := child.(type) {
		case *models.SimplePredicate:
			val = evaluateSimplePredicate(c, input, fieldTypes)
		case *models.CompoundPredicate:
			val = evaluateCompoundPredicate(c, input, fieldTypes)
		case *models.SimpleSetPredicate:
			val = evaluateSimpleSetPredicate(c, input, fieldTypes)
		}

		if surrogate && val != u {
			return val
		}

		if val == t {
			trueCount++
		} else if val == u {
			unknownCount++
		}
	}

	switch p.BooleanOperator {
	case models.CompoundPredicateOperatorAnd:
		if unknownCount > 0 && unknownCount+trueCount == len(p.Predicates) {
			return u
		}

		if trueCount == len(p.Predicates) {
			return t
		}
	case models.CompoundPredicateOperatorOr:
		if trueCount > 0 {
			return t
		}

		if unknownCount > 0 {
			return u
		}

	case models.CompoundPredicateOperatorXor:
		if unknownCount > 0 {
			return u
		}

		if trueCount%2 == 1 {
			return t
		}
	case models.CompoundPredicateOperatorSurrogate:
		return u
	}

	return f
}

func (m *TreeModel) Compile() error {
	for _, f := range m.model.MiningSchema.MiningFields {
		if f.UsageType == models.FieldUsageTypeTarget {
			m.targetField = f.Name
		}
	}

	m.fieldTypes = make(map[models.FieldName]models.DataType)

	for _, df := range m.dd.DataFields {
		m.fieldTypes[df.Name] = df.DataType
	}

	for _, tf := range m.model.LocalTransformations.DerivedFields {
		m.td.DerivedFields = append(m.td.DerivedFields, tf)
	}

	for _, df := range m.td.DerivedFields {
		m.fieldTypes[models.FieldName(df.Name)] = df.DataType
	}

	m.root = newNode(m, m.model.Node)

	m.compiled = true
	return nil
}

func (m *TreeModel) Verify() error {
	return verifyModel(m, m.model.ModelVerification)
}

func (m *TreeModel) Evaluate(input DataRow) (DataRow, error) {
	var err error

	if !m.validated {
		return nil, ErrNotValidated
	}

	if !m.compiled {
		return nil, ErrNotCompiled
	}

	input, err = HandleInput(input, m.dd, m.td, &m.model.LocalTransformations, &m.model.MiningSchema)
	if err != nil {
		return nil, err
	}

	scores, result := m.root.evaluate(input)

	if result == t {
		best := pickBest(scores)
		out := make(DataRow)

		if m.model.FunctionName == models.MiningFunctionClassification {
			out[string(m.targetField)] = NewValue(best.value)
			for _, score := range scores {
				key, ok := m.outKeys[score.value]
				if !ok {
					key = fmt.Sprintf("probability(%s)", score.value)
					m.outKeys[score.value] = key
				}
				
				out[key] = NewValue(score.probability)
			}
		} else if m.model.FunctionName == models.MiningFunctionRegression {
			score, err := strconv.ParseFloat(scores[0].value, 64)
			if err != nil {
				return nil, err
			}

			out["score"] = NewValue(score)
		}

		return out, nil
	}

	return nil, nil
}
