import { useMemo } from 'react';
import type { CategoryCode } from '../api';
import { useCategories, useUsers } from './queries';

const capitalize = (s: string) => s.charAt(0).toUpperCase() + s.slice(1);

/** Select options and display names for users and categories, falling back to raw ids/codes. */
export function useLookups() {
  const { data: users } = useUsers();
  const { data: categories } = useCategories();

  return useMemo(() => {
    const userNames = new Map(users?.map((u) => [u.telegram_id, u.display_name]));
    const categoryLabels = new Map(categories?.map((c) => [c.code, capitalize(c.label)]));
    return {
      userOptions: (users ?? []).map((u) => ({ value: String(u.telegram_id), label: u.display_name })),
      categoryOptions: [...categoryLabels].map(([value, label]) => ({ value, label })),
      userName: (id: number) => userNames.get(id) ?? String(id),
      categoryLabel: (code: CategoryCode) => categoryLabels.get(code) ?? code,
    };
  }, [users, categories]);
}
