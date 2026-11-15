package load

import (
	"reflect"
	"testing"

	"github.com/dilaziz/blast/internal/model"
)

func TestFormatConfigReadsBackTheSame(t *testing.T) {
	prices := func(monthly, annual int64) map[model.Billing]int64 {
		return map[model.Billing]int64{model.Monthly: monthly, model.Annual: annual}
	}
	cfg := model.Config{
		Plans: map[string]model.Plan{
			"pro":        {Prices: prices(9900, 99000)},
			"legacy-pro": {Prices: prices(7950, 79500), Legacy: true},
			"free":       {Prices: map[model.Billing]int64{model.Monthly: 0}},
			// Names that YAML would read as something other than text.
			"123":  {Prices: prices(100, 1000)},
			"true": {Prices: prices(1, 10)},
			"null": {Prices: prices(2, 20)},
		},
		Discounts: []model.Discount{
			{Name: "WELCOME10", BasisPoints: 1000},
			{Name: "HALF", BasisPoints: 1250},
			{Name: "TENOFF", AmountCents: 1000},
			{Name: "50", BasisPoints: 5000},
			{Name: "a: b #c", AmountCents: 1},
			{Name: "it's {odd}, [very]", BasisPoints: 1},
			{Name: "REFERRAL20", BasisPoints: 2000, When: `billing == "annual" and referred`},
		},
	}
	data, err := FormatConfig(cfg, "A comment.")
	if err != nil {
		t.Fatal(err)
	}
	got, err := parseConfig("written.yml", data)
	if err != nil {
		t.Fatalf("%v\n%s", err, data)
	}
	if !reflect.DeepEqual(got.Plans, cfg.Plans) || !reflect.DeepEqual(got.Discounts, cfg.Discounts) {
		t.Errorf("read back differently:\n got  %+v\n want %+v\n%s", got, cfg, data)
	}

	if _, err := FormatConfig(model.Config{Payment: map[string]string{"payment.capture": "auto"}}, ""); err == nil {
		t.Error("a config with payment settings was written, silently dropping them")
	}
}

func TestFormatConfigLooksLikeTheExample(t *testing.T) {
	cfg := model.Config{
		Plans: map[string]model.Plan{
			"starter": {Prices: map[model.Billing]int64{model.Monthly: 2900, model.Annual: 29000}},
			"pro":     {Prices: map[model.Billing]int64{model.Monthly: 9950}},
		},
		Discounts: []model.Discount{{Name: "WELCOME10", BasisPoints: 1000}, {Name: "TENOFF", AmountCents: 1000}},
	}
	got, err := FormatConfig(cfg, "Today's pricing.")
	if err != nil {
		t.Fatal(err)
	}
	const want = `# Today's pricing.
plans:
  pro: {monthly: 99.5}
  starter: {monthly: 29, annual: 290}
discounts:
  WELCOME10: {percent: 10}
  TENOFF: {amount: 10}
`
	if string(got) != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}
