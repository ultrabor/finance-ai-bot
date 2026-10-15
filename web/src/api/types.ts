// Mirrors components.schemas in api/openapi.yaml.

export const CATEGORY_CODES = [
  'food',
  'transport',
  'housing',
  'entertainment',
  'health',
  'shopping',
  'other',
] as const;

export type CategoryCode = (typeof CATEGORY_CODES)[number];

export interface Category {
  code: CategoryCode;
  label: string;
}

export interface Expense {
  id: string;
  user_id: number;
  amount: number;
  category: CategoryCode;
  spent_at: string;
  comment: string | null;
  created_at: string;
  updated_at: string;
}

export interface ExpenseList {
  items: Expense[];
  total: number;
  sum: number;
}

export interface ExpenseCreate {
  user_id: number;
  amount: number;
  category: CategoryCode;
  spent_at: string;
  comment?: string | null;
}

export type ExpenseUpdate = Partial<Omit<ExpenseCreate, 'user_id'>>;

export interface ExpenseQuery {
  user_id?: number;
  from?: string;
  to?: string;
  category?: CategoryCode;
  q?: string;
  limit?: number;
  offset?: number;
}

export interface User {
  telegram_id: number;
  username: string | null;
  first_name: string;
  last_name: string | null;
  display_name: string;
  created_at: string;
}

export interface UserCreate {
  telegram_id: number;
  first_name: string;
  last_name?: string;
  username?: string;
}

export type ApiErrorCode =
  | 'validation_error'
  | 'not_found'
  | 'bad_request'
  | 'method_not_allowed'
  | 'payload_too_large'
  | 'internal_error';

export interface ApiErrorBody {
  error: {
    code: ApiErrorCode;
    message: string;
    field?: string;
  };
}

export interface ItemsResponse<T> {
  items: T[];
}
