package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// Expense holds the schema definition for the Expense entity.
type Expense struct {
	ent.Schema
}

// Fields of the Expense.
func (Expense) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("chat_id"),
		field.Float("amount"),
		field.String("description").Default(""),
		field.Int64("paid_by"),
		field.String("paid_by_name"),
		field.Bool("for_other").Default(false),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}
