import { useCallback, useEffect, useState } from 'react';
import { CATEGORY_CODES, type CategoryCode, type ExpenseQuery } from '../api';
import { isValidDate } from '../lib/dates';

export const PAGE_SIZE = 50;

export interface ExpenseFilters {
  userId: string | null;
  from: string | null;
  to: string | null;
  category: CategoryCode | null;
  q: string;
  page: number;
}

const POSITIVE_INT_RE = /^[1-9]\d*$/;

const isPositiveInt = (v: string | null): v is string =>
  v !== null && POSITIVE_INT_RE.test(v) && Number.isSafeInteger(Number(v));

/** Reads filters from a (possibly hand-edited) URL, dropping anything invalid. */
function parse(search: string): ExpenseFilters {
  const p = new URLSearchParams(search);
  const date = (key: string) => {
    const v = p.get(key);
    return v && isValidDate(v) ? v : null;
  };
  let from = date('from');
  let to = date('to');
  // An inverted range can't match anything; drop it rather than guess intent.
  // YYYY-MM-DD strings compare correctly as plain strings.
  if (from && to && from > to) from = to = null;
  const userId = p.get('user');
  const category = p.get('category');
  const page = p.get('page');
  return {
    userId: isPositiveInt(userId) ? userId : null,
    from,
    to,
    category: CATEGORY_CODES.includes(category as CategoryCode) ? (category as CategoryCode) : null,
    q: p.get('q') ?? '',
    page: isPositiveInt(page) ? Number(page) : 1,
  };
}

function serialize(f: ExpenseFilters): string {
  const p = new URLSearchParams();
  if (f.userId) p.set('user', f.userId);
  if (f.from) p.set('from', f.from);
  if (f.to) p.set('to', f.to);
  if (f.category) p.set('category', f.category);
  if (f.q) p.set('q', f.q);
  if (f.page > 1) p.set('page', String(f.page));
  const qs = p.toString();
  return qs ? `?${qs}` : '';
}

export function toExpenseQuery(f: ExpenseFilters): ExpenseQuery {
  return {
    user_id: f.userId ? Number(f.userId) : undefined,
    from: f.from ?? undefined,
    to: f.to ?? undefined,
    category: f.category ?? undefined,
    q: f.q.trim() || undefined,
    limit: PAGE_SIZE,
    offset: (f.page - 1) * PAGE_SIZE,
  };
}

/** Filter state mirrored into the URL query string so a reload keeps it. */
export function useExpenseFilters() {
  const [filters, setFiltersState] = useState(() => parse(window.location.search));

  useEffect(() => {
    const search = serialize(filters);
    if (search !== window.location.search) {
      window.history.replaceState(null, '', `${window.location.pathname}${search}`);
    }
  }, [filters]);

  /** Any change other than `page` itself sends the user back to page 1. */
  const setFilters = useCallback((patch: Partial<ExpenseFilters>) => {
    setFiltersState((prev) => ({ ...prev, page: 1, ...patch }));
  }, []);

  return { filters, setFilters };
}
