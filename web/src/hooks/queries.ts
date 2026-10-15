import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, type ExpenseCreate, type ExpenseQuery, type ExpenseUpdate, type UserCreate } from '../api';
import { isNotFound } from '../lib/notify';

const keys = {
  users: ['users'] as const,
  categories: ['categories'] as const,
  expenses: ['expenses'] as const,
  expenseList: (query: ExpenseQuery) => ['expenses', query] as const,
};

export function useUsers() {
  return useQuery({ queryKey: keys.users, queryFn: api.listUsers });
}

export function useCategories() {
  return useQuery({ queryKey: keys.categories, queryFn: api.listCategories, staleTime: Infinity });
}

export function useExpenses(query: ExpenseQuery) {
  return useQuery({
    queryKey: keys.expenseList(query),
    queryFn: () => api.listExpenses(query),
    placeholderData: keepPreviousData,
    // Expenses also arrive via the Telegram bot, so keep the list fresh. The
    // interval pauses while the tab is hidden (refetchIntervalInBackground=false).
    refetchOnWindowFocus: true,
    refetchInterval: 30_000,
  });
}

function useInvalidate(key: readonly string[]) {
  const queryClient = useQueryClient();
  return () => queryClient.invalidateQueries({ queryKey: key });
}

/** A 404 on an expense mutation means the row is stale: refresh the list so it disappears. */
function useInvalidateOnNotFound(key: readonly string[]) {
  const invalidate = useInvalidate(key);
  return (error: unknown) => {
    if (isNotFound(error)) void invalidate();
  };
}

export function useCreateExpense() {
  const onSuccess = useInvalidate(keys.expenses);
  return useMutation({ mutationFn: (body: ExpenseCreate) => api.createExpense(body), onSuccess });
}

export function useUpdateExpense() {
  const onSuccess = useInvalidate(keys.expenses);
  const onError = useInvalidateOnNotFound(keys.expenses);
  return useMutation({
    mutationFn: ({ id, body }: { id: string; body: ExpenseUpdate }) => api.updateExpense(id, body),
    onSuccess,
    onError,
  });
}

export function useDeleteExpense() {
  const onSuccess = useInvalidate(keys.expenses);
  const onError = useInvalidateOnNotFound(keys.expenses);
  return useMutation({ mutationFn: (id: string) => api.deleteExpense(id), onSuccess, onError });
}

export function useCreateUser() {
  const onSuccess = useInvalidate(keys.users);
  return useMutation({ mutationFn: (body: UserCreate) => api.createUser(body), onSuccess });
}
