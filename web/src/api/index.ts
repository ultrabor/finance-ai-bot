import { request } from './client';
import type {
  Category,
  Expense,
  ExpenseCreate,
  ExpenseList,
  ExpenseQuery,
  ExpenseUpdate,
  ItemsResponse,
  User,
  UserCreate,
} from './types';

export { ApiError } from './client';
export * from './types';

export const api = {
  listExpenses: (query: ExpenseQuery) => request<ExpenseList>('/expenses', { query: { ...query } }),
  createExpense: (body: ExpenseCreate) => request<Expense>('/expenses', { method: 'POST', body }),
  updateExpense: (id: string, body: ExpenseUpdate) =>
    request<Expense>(`/expenses/${encodeURIComponent(id)}`, { method: 'PATCH', body }),
  deleteExpense: (id: string) => request<void>(`/expenses/${encodeURIComponent(id)}`, { method: 'DELETE' }),

  listUsers: () => request<ItemsResponse<User>>('/users').then((r) => r.items),
  createUser: (body: UserCreate) => request<User>('/users', { method: 'POST', body }),

  listCategories: () => request<ItemsResponse<Category>>('/categories').then((r) => r.items),
};
