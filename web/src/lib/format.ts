import dayjs from 'dayjs';

const money = new Intl.NumberFormat('ru-RU', {
  style: 'currency',
  currency: 'RUB',
  minimumFractionDigits: 2,
  maximumFractionDigits: 2,
});

/** 1200.5 -> "1 200,50 ₽" */
export function formatMoney(amount: number): string {
  return money.format(amount);
}

/** "2026-10-15" -> "15.10.2026" */
export function formatDate(isoDate: string): string {
  return dayjs(isoDate).format('DD.MM.YYYY');
}

const pluralRules = new Intl.PluralRules('ru-RU');

/** pluralize(3, ['трата', 'траты', 'трат']) -> "траты" */
export function pluralize(n: number, [one, few, many]: [string, string, string]): string {
  switch (pluralRules.select(n)) {
    case 'one':
      return one;
    case 'few':
      return few;
    default:
      return many;
  }
}
