import { useEffect, useState } from 'react';
import { Button, Group, Select, SimpleGrid, Stack, TextInput } from '@mantine/core';
import { DatePickerInput } from '@mantine/dates';
import { useDebouncedValue } from '@mantine/hooks';
import type { CategoryCode } from '../api';
import type { ExpenseFilters } from '../hooks/useExpenseFilters';
import { useLookups } from '../hooks/useLookups';
import { DATE_PRESETS, matchPreset } from '../lib/dates';
import { SearchIcon } from './icons';

const ALL = 'all';
const SEARCH_DEBOUNCE_MS = 400;

interface Props {
  filters: ExpenseFilters;
  onChange: (patch: Partial<ExpenseFilters>) => void;
}

export function FiltersBar({ filters, onChange }: Props) {
  const { userOptions, categoryOptions } = useLookups();
  const [search, setSearch] = useState(filters.q);
  const [debouncedSearch] = useDebouncedValue(search, SEARCH_DEBOUNCE_MS);

  useEffect(() => {
    if (debouncedSearch !== filters.q) onChange({ q: debouncedSearch });
  }, [debouncedSearch, filters.q, onChange]);

  // Range mode reports [from, null] after the first click; keep that in a local
  // draft and commit only complete (or fully cleared) ranges to the filters.
  const [range, setRange] = useState<[string | null, string | null]>([filters.from, filters.to]);
  const [synced, setSynced] = useState({ from: filters.from, to: filters.to });
  if (synced.from !== filters.from || synced.to !== filters.to) {
    // Filters changed externally (preset, URL): reset the draft to match.
    setSynced({ from: filters.from, to: filters.to });
    setRange([filters.from, filters.to]);
  }

  const handleRangeChange = ([from, to]: [string | null, string | null]) => {
    setRange([from, to]);
    if ((from && to) || (!from && !to)) onChange({ from, to });
  };

  // Dropdown closed with half a range picked: show the range that is actually applied.
  const handleDropdownClose = () => {
    if (!range[0] !== !range[1]) setRange([filters.from, filters.to]);
  };

  const activePreset = matchPreset(filters);

  return (
    <Stack gap="sm">
      <SimpleGrid cols={{ base: 1, sm: 2, lg: 4 }} spacing="sm">
        <Select
          label="Член семьи"
          data={[{ value: ALL, label: 'Все' }, ...userOptions]}
          value={filters.userId ?? ALL}
          onChange={(v) => onChange({ userId: v === ALL ? null : v })}
          allowDeselect={false}
          searchable
        />
        <DatePickerInput
          type="range"
          label="Период"
          placeholder="Всё время"
          valueFormat="DD.MM.YYYY"
          value={range}
          onChange={handleRangeChange}
          onDropdownClose={handleDropdownClose}
          // Second click on the same day selects a one-day range instead of clearing the filter.
          allowSingleDateInRange
          clearable
        />
        <Select
          label="Категория"
          data={[{ value: ALL, label: 'Все' }, ...categoryOptions]}
          value={filters.category ?? ALL}
          onChange={(v) => onChange({ category: v === ALL ? null : (v as CategoryCode) })}
          allowDeselect={false}
        />
        <TextInput
          label="Поиск по комментарию"
          placeholder="Например, такси"
          leftSection={<SearchIcon />}
          value={search}
          onChange={(e) => setSearch(e.currentTarget.value)}
        />
      </SimpleGrid>
      <Group gap="xs">
        {DATE_PRESETS.map((preset) => (
          <Button
            key={preset.key}
            size="xs"
            variant={activePreset === preset.key ? 'filled' : 'default'}
            onClick={() => onChange(preset.range())}
          >
            {preset.label}
          </Button>
        ))}
      </Group>
    </Stack>
  );
}
