package tools

import "context"

const DeleteExpense = "delete_expense"

type deleteArgs struct {
	ID string `json:"id"`
}

func (s *Set) deleteTool() tool {
	return tool{
		def: definition(DeleteExpense, "Удалить трату пользователя; id бери из list_expenses.", `{
			"type": "object",
			"properties": {
				"id": {"type": "string", "description": "id траты из list_expenses"}
			},
			"required": ["id"]
		}`),
		run: handle(s.delete),
	}
}

func (s *Set) delete(ctx context.Context, turn Turn, args deleteArgs) (any, error) {
	id, err := parseID(args.ID)
	if err != nil {
		return nil, err
	}
	// Удалённая трата возвращается, чтобы модель могла назвать, что именно удалено.
	e, err := s.expenses.Delete(ctx, turn.Scope, id)
	if err != nil {
		return nil, err
	}
	turn.Journal.Record("удалил", e)
	return mutationView{Deleted: s.view(e)}, nil
}
