package dto_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/piggystats/nodera/contexts/billing/aggregates/invoice"
	"github.com/piggystats/nodera/contexts/billing/application/dto"
)

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func marshal(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	require.NoError(t, err)

	return string(data)
}

func TestToUsageLines_ADropletLineKeepsItsFieldsAndGainsItsKind(t *testing.T) {
	dropletID := uuid.MustParse("0d7f0f43-27c5-4b34-9d0e-2b2b1f1f6a10")

	lines := dto.ToUsageLines([]invoice.Line{{
		Kind: invoice.LineDroplet, DropletID: dropletID, Hostname: "web-1", PlanCode: "c1-small", Hours: 3,
		HourlyPrice: d("0.2766"), Amount: d("0.83"),
	}})

	require.Len(t, lines, 1)
	assert.JSONEq(t, `{"kind":"droplet","droplet_id":"0d7f0f43-27c5-4b34-9d0e-2b2b1f1f6a10","hostname":"web-1",
		"plan_code":"c1-small","hours":3,"hourly_price":"0.2766","amount":"0.83"}`, marshal(t, lines[0]))
}

func TestToUsageLines_AMailLineOmitsTheDropletFields(t *testing.T) {
	projectID := uuid.MustParse("6f1c4c8e-1d3b-4b8e-9a52-0d6f5b1f0a01")

	lines := dto.ToUsageLines([]invoice.Line{{
		Kind: invoice.LineMail, ProjectID: projectID, ProjectName: "Shop", PackageCode: "10k", Amount: d("9"),
	}})

	require.Len(t, lines, 1)
	assert.JSONEq(t, `{"kind":"mail","project_id":"6f1c4c8e-1d3b-4b8e-9a52-0d6f5b1f0a01","project_name":"Shop",
		"package_code":"10k","amount":"9.00"}`, marshal(t, lines[0]))
}

func TestToInvoice_ListsBothKindsInTheirOrder(t *testing.T) {
	inv := &invoice.Invoice{Lines: []invoice.Line{
		{Kind: invoice.LineDroplet, DropletID: uuid.New(), Hostname: "web-1", PlanCode: "c1-small", Hours: 10, HourlyPrice: d("1"), Amount: d("10")},
		{Kind: invoice.LineMail, ProjectID: uuid.New(), ProjectName: "Shop", PackageCode: "50k", Amount: d("29")},
	}}

	out := dto.ToInvoice(inv, nil)

	require.Len(t, out.Lines, 2)
	assert.Equal(t, "droplet", out.Lines[0].Kind)
	assert.Equal(t, "mail", out.Lines[1].Kind)
	assert.Equal(t, "29.00", out.Lines[1].Amount)
}
