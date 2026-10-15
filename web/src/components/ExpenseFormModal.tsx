import { useState, type FormEvent } from 'react';
import { Button, Group, Modal, NumberInput, Select, Stack, Textarea } from '@mantine/core';
import { DatePickerInput } from '@mantine/dates';
import type { CategoryCode, Expense, ExpenseUpdate } from '../api';
import { useCreateExpense, useUpdateExpense } from '../hooks/queries';
import { useLookups } from '../hooks/useLookups';
import { today } from '../lib/dates';
import { handleFormError, isNotFound, notifySuccess, notifyWarning, type FieldErrors } from '../lib/notify';

const COMMENT_MAX = 500;
/** Backend limit per expense, in rubles (inclusive). */
const AMOUNT_MAX = 100_000_000;

/**
 * NumberInput reports a string (e.g. "99.90", "12.", or "99,90") until blur, and
 * Enter submits without blurring, so normalize to a number rounded to kopecks.
 */
function parseAmount(value: number | string): number | null {
  if (typeof value === 'string') {
    const normalized = value.replace(/\s/g, '').replace(',', '.');
    if (normalized === '') return null;
    value = Number(normalized);
  }
  return Number.isFinite(value) ? Math.round(value * 100) / 100 : null;
}
// Field names as the backend reports them in `error.field`.
const FIELDS = ['user_id', 'amount', 'category', 'spent_at', 'comment'] as const;
type Field = (typeof FIELDS)[number];

interface Props {
  opened: boolean;
  /** Expense being edited; `null` opens the form in create mode. */
  expense: Expense | null;
  onClose: () => void;
}

export function ExpenseFormModal({ opened, expense, onClose }: Props) {
  return (
    <Modal opened={opened} onClose={onClose} title={expense ? 'Изменить трату' : 'Новая трата'} centered>
      <ExpenseForm key={expense?.id ?? 'new'} expense={expense} onDone={onClose} />
    </Modal>
  );
}

function ExpenseForm({ expense, onDone }: { expense: Expense | null; onDone: () => void }) {
  const { userOptions, categoryOptions, userName } = useLookups();
  const create = useCreateExpense();
  const update = useUpdateExpense();

  const [userId, setUserId] = useState<string | null>(expense ? String(expense.user_id) : null);
  const [amount, setAmount] = useState<number | string>(expense?.amount ?? '');
  const [category, setCategory] = useState<string | null>(expense?.category ?? null);
  const [spentAt, setSpentAt] = useState<string | null>(expense?.spent_at ?? today());
  const [comment, setComment] = useState(expense?.comment ?? '');
  const [errors, setErrors] = useState<FieldErrors<Field>>({});

  // On edit the owner may be missing from /users; keep it selectable for display.
  const ownerOptions =
    expense && !userOptions.some((o) => o.value === userId)
      ? [{ value: String(expense.user_id), label: userName(expense.user_id) }, ...userOptions]
      : userOptions;

  const validate = (parsedAmount: number | null): FieldErrors<Field> => {
    const e: FieldErrors<Field> = {};
    if (!expense && !userId) e.user_id = 'Выберите члена семьи';
    if (parsedAmount === null || !(parsedAmount > 0)) e.amount = 'Сумма должна быть больше нуля';
    else if (parsedAmount > AMOUNT_MAX) e.amount = 'Слишком большая сумма (не больше 100 000 000 ₽)';
    if (!category) e.category = 'Выберите категорию';
    if (!spentAt) e.spent_at = 'Укажите дату';
    if (comment.length > COMMENT_MAX) e.comment = `Не длиннее ${COMMENT_MAX} символов`;
    return e;
  };

  const handleSubmit = (event: FormEvent) => {
    event.preventDefault();
    const parsedAmount = parseAmount(amount);
    const validation = validate(parsedAmount);
    setErrors(validation);
    if (Object.keys(validation).length > 0) return;

    const fields = {
      amount: parsedAmount as number,
      category: category as CategoryCode,
      spent_at: spentAt as string,
      comment: comment.trim() || null,
    };
    const callbacks = {
      onSuccess: () => {
        notifySuccess(expense ? 'Трата обновлена' : 'Трата добавлена');
        onDone();
      },
      onError: (error: unknown) => {
        if (isNotFound(error)) {
          notifyWarning('Трата не найдена — возможно, её удалили');
          onDone();
          return;
        }
        handleFormError(error, FIELDS, setErrors);
      },
    };

    if (expense) {
      // Send only changed fields so concurrent edits (e.g. from the bot) aren't overwritten.
      const body: ExpenseUpdate = {};
      if (fields.amount !== Math.round(expense.amount * 100) / 100) body.amount = fields.amount;
      if (fields.category !== expense.category) body.category = fields.category;
      if (fields.spent_at !== expense.spent_at) body.spent_at = fields.spent_at;
      // '' and null both mean "no comment".
      if ((fields.comment ?? '') !== (expense.comment ?? '')) body.comment = fields.comment;
      if (Object.keys(body).length === 0) {
        notifyWarning('Нет изменений');
        onDone();
        return;
      }
      update.mutate({ id: expense.id, body }, callbacks);
    }
    else create.mutate({ user_id: Number(userId), ...fields }, callbacks);
  };

  return (
    <form onSubmit={handleSubmit} noValidate>
      <Stack gap="sm">
        <Select
          label="Член семьи"
          placeholder="Выберите"
          data={ownerOptions}
          value={userId}
          onChange={setUserId}
          error={errors.user_id}
          readOnly={!!expense}
          description={expense ? 'Владельца траты изменить нельзя' : undefined}
          withAsterisk={!expense}
          searchable={!expense}
        />
        <NumberInput
          label="Сумма"
          placeholder="0,00"
          value={amount}
          onChange={setAmount}
          error={errors.amount}
          min={0}
          max={AMOUNT_MAX}
          clampBehavior="none"
          allowNegative={false}
          decimalScale={2}
          decimalSeparator=","
          thousandSeparator=" "
          rightSection="₽"
          hideControls
          withAsterisk
          data-autofocus
        />
        <Select
          label="Категория"
          placeholder="Выберите"
          data={categoryOptions}
          value={category}
          onChange={setCategory}
          error={errors.category}
          withAsterisk
        />
        <DatePickerInput
          label="Дата"
          valueFormat="DD.MM.YYYY"
          value={spentAt}
          onChange={setSpentAt}
          error={errors.spent_at}
          withAsterisk
        />
        <Textarea
          label="Комментарий"
          placeholder="Например, кофе"
          value={comment}
          onChange={(e) => setComment(e.currentTarget.value)}
          error={errors.comment}
          description={`${comment.length}/${COMMENT_MAX}`}
          maxLength={COMMENT_MAX}
          autosize
          minRows={2}
          maxRows={6}
        />
        <Group justify="flex-end" mt="sm">
          <Button variant="default" onClick={onDone}>
            Отмена
          </Button>
          <Button type="submit" loading={create.isPending || update.isPending}>
            Сохранить
          </Button>
        </Group>
      </Stack>
    </form>
  );
}
