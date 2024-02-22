package expr

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"
	"github.com/grafana/grafana/pkg/expr/mathexp"
	"github.com/grafana/grafana/pkg/expr/sql"
	"github.com/grafana/grafana/pkg/infra/tracing"
	"github.com/scottlepp/go-duck/duck"
)

// SQLCommand is an expression to run SQL over results
type SQLCommand struct {
	query       string
	varsToQuery []string
	timeRange   TimeRange
	refID       string
}

// NewSQLCommand creates a new SQLCommand.
func NewSQLCommand(refID, rawSQL string, tr TimeRange) (*SQLCommand, error) {
	tables, err := sql.TablesList(rawSQL)
	if err != nil {
		return nil, err
	}

	return &SQLCommand{
		query:       rawSQL,
		varsToQuery: tables,
		timeRange:   tr,
		refID:       refID,
	}, nil
}

// UnmarshalSQLCommand creates a SQLCommand from Grafana's frontend query.
func UnmarshalSQLCommand(rn *rawNode) (*SQLCommand, error) {
	if rn.TimeRange == nil {
		return nil, fmt.Errorf("time range must be specified for refID %s", rn.RefID)
	}

	expressionRaw, ok := rn.Query["expression"]
	if !ok {
		return nil, errors.New("no expression in the query")
	}
	expression, ok := expressionRaw.(string)
	if !ok {
		return nil, fmt.Errorf("expected prql expression to be type string, but got type %T", expressionRaw)
	}

	return NewSQLCommand(rn.RefID, expression, rn.TimeRange)
}

// NeedsVars returns the variable names (refIds) that are dependencies
// to execute the command and allows the command to fulfill the Command interface.
func (gr *SQLCommand) NeedsVars() []string {
	return gr.varsToQuery
}

// Execute runs the command and returns the results or an error if the command
// failed to execute.
func (gr *SQLCommand) Execute(ctx context.Context, now time.Time, vars mathexp.Vars, tracer tracing.Tracer) (mathexp.Results, error) {
	_, span := tracer.Start(ctx, "SSE.ExecuteSQL")
	defer span.End()

	allFrames := []*data.Frame{}
	for _, ref := range gr.varsToQuery {
		results := vars[ref]
		frames := results.Values.AsDataFrames(ref)
		allFrames = append(frames, frames...)
	}

	rsp := mathexp.Results{}

	duckDB := duck.NewInMemoryDB()
	var frame = &data.Frame{}
	_, err := duckDB.QueryFramesInto(gr.refID, gr.query, allFrames, frame)
	if err != nil {
		rsp.Error = err
		return rsp, nil
	}

	frame.RefID = gr.refID
	rsp.Values = mathexp.Values{
		mathexp.Scalar{Frame: frame},
	}
	return rsp, nil
}
