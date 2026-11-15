package sim

import (
	"github.com/dilaziz/blast/internal/model"
	"github.com/dilaziz/blast/internal/rules/expr"
)

// fact is one thing a condition can ask about a customer.
type fact struct {
	typ expr.Type
	get func(model.Customer, model.Plan) expr.Value
}

// facts is the single table behind both the schema conditions are checked
// against and the values they are evaluated with, so the two cannot drift.
var facts = func() map[string]fact {
	m := map[string]fact{
		"id":           {expr.Str, func(c model.Customer, _ model.Plan) expr.Value { return expr.StrOf(c.ID) }},
		"plan":         {expr.Str, func(c model.Customer, _ model.Plan) expr.Value { return expr.StrOf(c.Plan) }},
		"billing":      {expr.Str, func(c model.Customer, _ model.Plan) expr.Value { return expr.StrOf(string(c.Billing)) }},
		"referred":     {expr.Bool, func(c model.Customer, _ model.Plan) expr.Value { return expr.BoolOf(c.Referred) }},
		"legacy":       {expr.Bool, func(_ model.Customer, p model.Plan) expr.Value { return expr.BoolOf(p.Legacy) }},
		"coupon_count": {expr.Num, func(c model.Customer, _ model.Plan) expr.Value { return expr.CountOf(len(c.Coupons)) }},
	}
	// A discount's `when:` reads naturally with bare names; rules read better
	// qualified. Both spellings mean the same fact.
	for qualified, bare := range map[string]string{
		"customer.id": "id", "customer.plan": "plan", "customer.billing": "billing",
		"customer.referred": "referred", "customer.coupon_count": "coupon_count",
		"plan.name": "plan", "plan.legacy": "legacy",
	} {
		m[qualified] = m[bare]
	}
	return m
}()

// Schema declares the customer facts a condition may read. Each call returns
// a fresh schema the caller is free to extend.
func Schema() expr.Schema {
	vars := make(map[string]expr.Type, len(facts))
	for name, f := range facts {
		vars[name] = f.typ
	}
	return expr.Schema{Vars: vars}
}

// Facts presents one customer, on one plan, to a condition compiled against
// Schema.
type Facts struct {
	Customer model.Customer
	Plan     model.Plan
}

func (f Facts) Var(name string) expr.Value { return facts[name].get(f.Customer, f.Plan) }

// Call exists to satisfy expr.Env; Schema declares no functions.
func (Facts) Call(string, []expr.Value) expr.Value { return expr.Value{} }
