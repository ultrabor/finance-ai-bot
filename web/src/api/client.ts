import type { ApiErrorBody, ApiErrorCode } from './types';

const BASE_URL = '/api/v1';

/** `network_error` is client-side only: the request never got a response. */
export type ClientErrorCode = ApiErrorCode | 'network_error';

export class ApiError extends Error {
  readonly code: ClientErrorCode;
  readonly status: number;
  readonly field?: string;

  constructor(code: ClientErrorCode, message: string, status: number, field?: string) {
    super(message);
    this.name = 'ApiError';
    this.code = code;
    this.status = status;
    this.field = field;
  }
}

type QueryValue = string | number | undefined | null;

interface RequestOptions {
  method?: 'GET' | 'POST' | 'PATCH' | 'DELETE';
  query?: Record<string, QueryValue>;
  body?: unknown;
}

function buildUrl(path: string, query?: Record<string, QueryValue>): string {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(query ?? {})) {
    if (value !== undefined && value !== null && value !== '') params.set(key, String(value));
  }
  const qs = params.toString();
  return `${BASE_URL}${path}${qs ? `?${qs}` : ''}`;
}

function isErrorBody(value: unknown): value is ApiErrorBody {
  const error = (value as ApiErrorBody | null)?.error;
  return typeof error?.code === 'string' && typeof error.message === 'string';
}

export async function request<T>(path: string, { method = 'GET', query, body }: RequestOptions = {}): Promise<T> {
  let response: Response;
  try {
    response = await fetch(buildUrl(path, query), {
      method,
      headers: body !== undefined ? { 'Content-Type': 'application/json' } : undefined,
      body: body !== undefined ? JSON.stringify(body) : undefined,
    });
  } catch {
    throw new ApiError('network_error', 'Сервер недоступен. Проверьте соединение.', 0);
  }

  if (response.status === 204) return undefined as T;

  const data: unknown = await response.json().catch(() => null);

  if (!response.ok) {
    if (isErrorBody(data)) {
      const { code, message, field } = data.error;
      throw new ApiError(code, message, response.status, field);
    }
    // Non-JSON error body: the API itself always answers JSON (including /api 404/405),
    // but a proxy in front of it can return plain text or HTML (e.g. nginx 502/504).
    const code = response.status >= 500 ? 'internal_error' : 'bad_request';
    const label = response.status >= 500 ? 'Ошибка сервера' : 'Ошибка запроса';
    throw new ApiError(code, `${label} (${response.status})`, response.status);
  }

  return data as T;
}
