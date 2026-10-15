import dayjs from 'dayjs';
import customParseFormat from 'dayjs/plugin/customParseFormat';

dayjs.extend(customParseFormat);

export const DATE_FORMAT = 'YYYY-MM-DD';

/** Strict check: "2026-02-30" or "2026-1-5" are rejected. */
export function isValidDate(value: string): boolean {
  return dayjs(value, DATE_FORMAT, true).isValid();
}

export function today(): string {
  return dayjs().format(DATE_FORMAT);
}

export interface DateRange {
  from: string | null;
  to: string | null;
}

export interface DatePreset {
  key: string;
  label: string;
  range: () => DateRange;
}

export const DATE_PRESETS: DatePreset[] = [
  { key: 'today', label: 'Сегодня', range: () => ({ from: today(), to: today() }) },
  {
    key: 'week',
    label: 'Неделя',
    range: () => ({ from: dayjs().subtract(6, 'day').format(DATE_FORMAT), to: today() }),
  },
  {
    key: 'month',
    label: 'Этот месяц',
    range: () => ({
      from: dayjs().startOf('month').format(DATE_FORMAT),
      to: dayjs().endOf('month').format(DATE_FORMAT),
    }),
  },
  {
    key: 'prev-month',
    label: 'Прошлый месяц',
    range: () => {
      const prev = dayjs().subtract(1, 'month');
      return {
        from: prev.startOf('month').format(DATE_FORMAT),
        to: prev.endOf('month').format(DATE_FORMAT),
      };
    },
  },
  { key: 'all', label: 'Всё время', range: () => ({ from: null, to: null }) },
];

export function matchPreset({ from, to }: DateRange): string | undefined {
  return DATE_PRESETS.find((p) => {
    const r = p.range();
    return r.from === from && r.to === to;
  })?.key;
}
