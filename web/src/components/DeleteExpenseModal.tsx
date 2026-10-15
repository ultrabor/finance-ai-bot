import { Button, Group, Modal, Stack, Text } from '@mantine/core';
import type { Expense } from '../api';
import { useDeleteExpense } from '../hooks/queries';
import { useLookups } from '../hooks/useLookups';
import { formatDate, formatMoney } from '../lib/format';
import { isNotFound, notifyError, notifySuccess, notifyWarning } from '../lib/notify';

interface Props {
  opened: boolean;
  expense: Expense | null;
  onClose: () => void;
}

export function DeleteExpenseModal({ opened, expense, onClose }: Props) {
  const { categoryLabel, userName } = useLookups();
  const remove = useDeleteExpense();

  const handleConfirm = () => {
    if (!expense) return;
    remove.mutate(expense.id, {
      onSuccess: () => {
        notifySuccess('Трата удалена');
        onClose();
      },
      onError: (error) => {
        if (isNotFound(error)) {
          notifyWarning('Трата уже удалена');
          onClose();
          return;
        }
        notifyError(error, 'Не удалось удалить');
      },
    });
  };

  return (
    <Modal opened={opened} onClose={onClose} title="Удалить трату?" centered>
      {expense && (
        <Stack gap="md">
          <Text size="sm">
            {formatDate(expense.spent_at)} · {userName(expense.user_id)} · {categoryLabel(expense.category)} ·{' '}
            <b>{formatMoney(expense.amount)}</b>
            {expense.comment && <> · {expense.comment}</>}
          </Text>
          <Text size="sm" c="dimmed">
            Это действие нельзя отменить.
          </Text>
          <Group justify="flex-end">
            <Button variant="default" onClick={onClose}>
              Отмена
            </Button>
            <Button color="red" onClick={handleConfirm} loading={remove.isPending}>
              Удалить
            </Button>
          </Group>
        </Stack>
      )}
    </Modal>
  );
}
