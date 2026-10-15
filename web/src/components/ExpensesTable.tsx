import { ActionIcon, Group, Table, Text } from '@mantine/core';
import type { Expense } from '../api';
import { useLookups } from '../hooks/useLookups';
import { formatDate, formatMoney } from '../lib/format';
import { PencilIcon, TrashIcon } from './icons';

interface Props {
  items: Expense[];
  onEdit: (expense: Expense) => void;
  onDelete: (expense: Expense) => void;
}

export function ExpensesTable({ items, onEdit, onDelete }: Props) {
  const { userName, categoryLabel } = useLookups();

  return (
    <Table.ScrollContainer minWidth={760} type="native">
      <Table striped highlightOnHover verticalSpacing="xs">
        <Table.Thead>
          <Table.Tr>
            <Table.Th>Дата</Table.Th>
            <Table.Th>Член семьи</Table.Th>
            <Table.Th>Категория</Table.Th>
            <Table.Th ta="right">Сумма</Table.Th>
            <Table.Th>Комментарий</Table.Th>
            <Table.Th w={90} />
          </Table.Tr>
        </Table.Thead>
        <Table.Tbody>
          {items.map((e) => (
            <Table.Tr key={e.id}>
              <Table.Td style={{ whiteSpace: 'nowrap' }}>{formatDate(e.spent_at)}</Table.Td>
              <Table.Td>{userName(e.user_id)}</Table.Td>
              <Table.Td>{categoryLabel(e.category)}</Table.Td>
              <Table.Td ta="right" fw={500} style={{ whiteSpace: 'nowrap' }}>
                {formatMoney(e.amount)}
              </Table.Td>
              <Table.Td>
                {e.comment ? (
                  <Text size="sm" lineClamp={2} title={e.comment}>
                    {e.comment}
                  </Text>
                ) : (
                  <Text size="sm" c="dimmed">
                    —
                  </Text>
                )}
              </Table.Td>
              <Table.Td>
                <Group gap={4} justify="flex-end" wrap="nowrap">
                  <ActionIcon variant="subtle" aria-label="Изменить" onClick={() => onEdit(e)}>
                    <PencilIcon />
                  </ActionIcon>
                  <ActionIcon variant="subtle" color="red" aria-label="Удалить" onClick={() => onDelete(e)}>
                    <TrashIcon />
                  </ActionIcon>
                </Group>
              </Table.Td>
            </Table.Tr>
          ))}
        </Table.Tbody>
      </Table>
    </Table.ScrollContainer>
  );
}
