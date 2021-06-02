package evaluation

import (
	"errors"
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

	outputField models.FieldName
}

type test func(DataRow) predicateResult

type scoreDist struct {
	value       string
	confidence  float64
	probability float64
	recordCount float64
}

type node struct {
	id string
	defaultChild string
	children []node

	scoreDist []scoreDist

	test  test
	score scoreDist
	m     *TreeModel
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
	best := scoreDist{recordCount: 0.0}
	for _, score := range scores {
		if score.recordCount > best.recordCount {
			best = score
		}
	}

	return best
}

func (n node) evaluate(input DataRow) (scoreDist, predicateResult) {
	switch n.m.model.MissingValueStrategy {
	case models.MissingValueStrategyNullPrediction:
		return n.evaluateMissingValueStrategyNullPrediction(input)
	case models.MissingValueStrategyAggregateNodes:
		return n.evaluateMissingValueStrategyAggregateNodes(input)
	case models.MissingValueStrategyWeightedConfidence:
		scores, result := n.evaluateMissingValueStrategyWeightedConfidence(input)
		return pickBest(scores), result
	case models.MissingValueStrategyDefaultChild:
		return n.evaluateMissingValueStrategyDefaultChild(input)
	case models.MissingValueStrategyLastPrediction:
		return n.evaluateMissingValueStrategyLastPrediction(input)
	default:
		return n.evaluateMissingValueStrategyNone(input)
	}
}

func (n node) evaluateMissingValueStrategyNone(input DataRow) (scoreDist, predicateResult) {
	result := n.test(input)

	if result == f || result == u {
		return scoreDist{}, f
	}

	for _, c := range n.children {
		score, childResult := c.evaluateMissingValueStrategyNullPrediction(input)

		if childResult == t {
			return score, childResult
		}
	}

	score := n.score

	for _, sc := range n.scoreDist {
		if score.value == sc.value {
			return sc, result
		}
	}

	return score, result
}

func (n node) evaluateMissingValueStrategyLastPrediction(input DataRow) (scoreDist, predicateResult) {
	result := n.test(input)

	if result == f {
		return scoreDist{confidence: 1.0}, result
	}

	unknownValues := false
	var trueValue *scoreDist

	for _, c := range n.children {
		score, childResult := c.evaluateMissingValueStrategyLastPrediction(input)

		if childResult == u {
			unknownValues = true
		}

		if childResult == t {
			trueValue = &score
		}
	}

	if !unknownValues && trueValue != nil {
		return *trueValue, t
	}

	score := n.score

	for _, sc := range n.scoreDist {
		if score.value == sc.value {
			return sc, result
		}
	}

	return score, result
}

func (n node) evaluateMissingValueStrategyDefaultChild(input DataRow) (scoreDist, predicateResult) {
	result := n.test(input)

	if result == f {
		return scoreDist{confidence: 1.0}, result
	}

	childScores := make(map[string]scoreDist)
	unknownValues := false
	var trueValue *scoreDist

	for _, c := range n.children {
		score, childResult := c.evaluateMissingValueStrategyDefaultChild(input)
		childScores[c.id] = score

		if childResult == u {
			unknownValues = true
		}

		if childResult == t {
			trueValue = &score
		}
	}

	if !unknownValues && trueValue != nil {
		return *trueValue, t
	}

	if unknownValues {
		score, ok := childScores[n.defaultChild]
		if !ok {
			return scoreDist{}, u
		}

		score.confidence = score.confidence * float64(n.m.model.MissingValuePenalty)
		return score, t
	}

	score := n.score

	for _, sc := range n.scoreDist {
		if score.value == sc.value {
			return sc, result
		}
	}

	return score, result
}

func (n node) evaluateMissingValueStrategyNullPrediction(input DataRow) (scoreDist, predicateResult) {
	result := n.test(input)

	if result == f {
		return scoreDist{confidence: 1.0}, result
	}

	if result == u  {
		return scoreDist{confidence: 1.0}, result
	}

	for _, c := range n.children {
		score, childResult := c.evaluateMissingValueStrategyNullPrediction(input)

		if childResult == t {
			return score, childResult
		}

		if childResult == u {
			return scoreDist{confidence: 1.0}, childResult
		}
	}

	score := n.score

	for _, sc := range n.scoreDist {
		if score.value == sc.value {
			return sc, result
		}
	}

	return score, result
}

func (n node) evaluateMissingValueStrategyWeightedConfidence(input DataRow) ([]scoreDist, predicateResult) {
	result := n.test(input)

	if result == f {
		return n.scoreDist, result
	}

	childScores := make([]scoreDist, 0, 0)
	unknownValues := false
	var trueValue []scoreDist

	for _, c := range n.children {
		scores, childResult := c.evaluateMissingValueStrategyWeightedConfidence(input)

		if childResult != f {
			childScores = append(childScores, scores...)
		}

		if childResult == u {
			unknownValues = true
		} else if childResult == t {
			trueValue = scores
		}
	}

	if !unknownValues && len(trueValue) > 0 {
		return trueValue, result
	}

	if unknownValues {
		aggScore := scoreDist{confidence: 0.0, recordCount: 0.0}
		recordCount := 0.0

		scores := make(map[string][]scoreDist)

		for _, sc := range childScores {
			scores[sc.value] = append(scores[sc.value], sc)

			if sc.recordCount > aggScore.recordCount {
				aggScore = sc
			}

			recordCount = recordCount + sc.recordCount
		}

		best, ok := scores[aggScore.value]
		if !ok {
			return n.scoreDist, result
		}

		aggScore.probability = 0.0
		aggScore.confidence = 0.0

		for _, score := range best {
			cnt := score.recordCount / score.probability
			prob := score.probability * (cnt / recordCount)

			aggScore.probability += prob
			aggScore.confidence += prob
		}

		return []scoreDist{aggScore}, result
	}

	return n.scoreDist, result
}

func (n node) evaluateMissingValueStrategyAggregateNodes(input DataRow) (scoreDist, predicateResult) {
	result := n.test(input)

	if result == f {
		return n.score, result
	}

	childScores := make([]scoreDist, 0, 0)
	unknownValues := false
	var trueValue *scoreDist

	for _, c := range n.children {
		score, childResult := c.evaluateMissingValueStrategyAggregateNodes(input)
		childScores = append(childScores, score)

		if childResult == u {
			unknownValues = true
		} else if childResult == t {
			trueValue = &score
		}
	}

	if !unknownValues && trueValue != nil {
		return *trueValue, result
	}

	if unknownValues {
		aggScore := scoreDist{confidence: 0.0, recordCount: 0.0}
		recordCount := 0.0

		for _, sc := range childScores {
			if sc.recordCount > aggScore.recordCount {
				aggScore = sc
			}

			recordCount = recordCount + (sc.recordCount / sc.probability)
		}

		aggScore.probability = aggScore.recordCount / recordCount
		aggScore.confidence = aggScore.recordCount / recordCount
		return aggScore, t
	}

	score := n.score

	for _, sc := range n.scoreDist {
		if score.value == sc.value {
			return sc, result
		}
	}

	return score, result
}

func NewTreeModel(dd *models.DataDictionary, td *models.TransformationDictionary, model *models.TreeModel) (*TreeModel, error) {
	m := new(TreeModel)
	m.dd = dd
	m.td = td
	m.model = model

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
		id: modelNode.ID,
		defaultChild: modelNode.DefaultChild,
		m: m,
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

	rawPredicateValue, err := getRawValue(predicateValueType, p.Value)
	if err != nil {
		return f
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
		if val.Raw() == rawPredicateValue {
			return t
		}
	case models.SimplePredicateOperatorNotEqual:
		if val.Raw() != rawPredicateValue {
			return t
		}
	case models.SimplePredicateOperatorGreaterOrEqual:
		switch predicateValueType {
		case models.DataTypeDouble:
			if val.Float64() >= rawPredicateValue.(float64) {
				return t
			}
		case models.DataTypeFloat:
			if val.Float64() >= rawPredicateValue.(float64) {
				return t
			}
		case models.DataTypeInteger:
			if val.Int64() >= rawPredicateValue.(int64) {
				return t
			}
		}
	case models.SimplePredicateOperatorGreaterThan:
		switch predicateValueType {
		case models.DataTypeDouble:
			if val.Float64() > rawPredicateValue.(float64) {
				return t
			}
		case models.DataTypeFloat:
			if val.Float64() > rawPredicateValue.(float64) {
				return t
			}
		case models.DataTypeInteger:
			if val.Int64() > rawPredicateValue.(int64) {
				return t
			}
		}
	case models.SimplePredicateOperatorLessOrEqual:
		switch predicateValueType {
		case models.DataTypeDouble:
			if val.Float64() <= rawPredicateValue.(float64) {
				return t
			}
		case models.DataTypeFloat:
			if val.Float64() <= rawPredicateValue.(float64) {
				return t
			}
		case models.DataTypeInteger:
			if val.Int64() <= rawPredicateValue.(int64) {
				return t
			}
		}
	case models.SimplePredicateOperatorLessThan:
		switch predicateValueType {
		case models.DataTypeDouble:
			if val.Float64() < rawPredicateValue.(float64) {
				return t
			}
		case models.DataTypeFloat:
			if val.Float64() < rawPredicateValue.(float64) {
				return t
			}
		case models.DataTypeInteger:
			if val.Int64() < rawPredicateValue.(int64) {
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
			m.outputField = f.Name
		}
	}

	fieldTypes := map[models.FieldName]models.DataType{}

	for _, df := range m.dd.DataFields {
		fieldTypes[df.Name] = df.DataType
	}

	m.fieldTypes = fieldTypes

	m.root = newNode(m, m.model.Node)

	m.compiled = true
	return nil
}

func (m *TreeModel) Verify() error {
	if m.model.ModelVerification == nil {
		return nil
	}

	return ErrNotImplemented
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

	score, result := m.root.evaluate(input)

	//println(fmt.Sprintf("%v", score))

	if result == t {
		return DataRow{
			string(m.outputField): NewValue(score.value),
			"confidence":		   NewValue(score.confidence),
		}, nil
	}

	return nil, nil
}
