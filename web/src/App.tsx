import { useEffect, useState } from 'react';
import {
  Alert,
  Anchor,
  Box,
  Button,
  Center,
  Container,
  EmptyState,
  Group,
  Loader,
  LoadingOverlay,
  Pagination,
  Paper,
  Stack,
  Text,
  Title,
} from '@mantine/core';
import { useDisclosure } from '@mantine/hooks';
import type { Expense } from './api';
import { AddUserModal } from './components/AddUserModal';
import { DeleteExpenseModal } from './components/DeleteExpenseModal';
import { ExpenseFormModal } from './components/ExpenseFormModal';
import { ExpensesTable } from './components/ExpensesTable';
import { FiltersBar } from './components/FiltersBar';
import { useExpenses, useUsers } from './hooks/queries';
import { PAGE_SIZE, toExpenseQuery, useExpenseFilters, type ExpenseFilters } from './hooks/useExpenseFilters';
import { formatMoney, pluralize } from './lib/format';

const hasActiveFilters = (f: ExpenseFilters) => Boolean(f.userId || f.from || f.to || f.category || f.q);

export function App() {
  const { filters, setFilters } = useExpenseFilters();
  const users = useUsers();
  const expenses = useExpenses(toExpenseQuery(filters));

  const [formOpened, form] = useDisclosure(false);
  const [deleteOpened, deleteModal] = useDisclosure(false);
  const [userModalOpened, userModal] = useDisclosure(false);
  const [editing, setEditing] = useState<Expense | null>(null);
  const [deleting, setDeleting] = useState<Expense | null>(null);

  const data = expenses.data;
  const stale = expenses.isPlaceholderData;
  const totalPages = data ? Math.max(1, Math.ceil(data.total / PAGE_SIZE)) : 1;
  const noUsers = users.isSuccess && users.data.length === 0;

  // After deleting the last row on the last page, step back to a page that exists.
  useEffect(() => {
    if (data && !stale && data.items.length === 0 && filters.page > totalPages) setFilters({ page: totalPages });
  }, [data, stale, filters.page, totalPages, setFilters]);

  const openCreate = () => {
    setEditing(null);
    form.open();
  };
  const openEdit = (expense: Expense) => {
    setEditing(expense);
    form.open();
  };
  const openDelete = (expense: Expense) => {
    setDeleting(expense);
    deleteModal.open();
  };

  return (
    <Container size="xl" py="lg" px={{ base: 'sm', sm: 'md' }}>
      <Stack gap="md">
        <Group justify="space-between" wrap="wrap" gap="sm">
          <Title order={2}>Расходы семьи</Title>
          <Group gap="xs">
            <Button variant="default" onClick={userModal.open}>
              Добавить члена семьи
            </Button>
            <Button onClick={openCreate} disabled={noUsers}>
              Добавить трату
            </Button>
          </Group>
        </Group>

        {noUsers && (
          <Alert color="blue" title="Пока нет членов семьи">
            Члены семьи появятся после первого сообщения боту. Можно также{' '}
            <Anchor component="button" type="button" onClick={userModal.open} inherit>
              добавить члена семьи вручную
            </Anchor>
            .
          </Alert>
        )}
        {users.isError && (
          <Alert color="red" title="Не удалось загрузить членов семьи">
            {users.error.message}
          </Alert>
        )}

        <Paper withBorder p="md" radius="md">
          <FiltersBar filters={filters} onChange={setFilters} />
        </Paper>

        <Paper withBorder radius="md" p={{ base: 'xs', sm: 'md' }}>
          {expenses.isPending ? (
            <Center py="xl">
              <Loader />
            </Center>
          ) : expenses.isError && !data ? (
            <Alert color="red" title="Не удалось загрузить траты">
              <Stack gap="xs" align="flex-start">
                <Text size="sm">{expenses.error.message}</Text>
                <Button size="xs" variant="light" color="red" onClick={() => expenses.refetch()}>
                  Повторить
                </Button>
              </Stack>
            </Alert>
          ) : data ? (
            <Stack gap="md">
              {expenses.isRefetchError && (
                <Alert color="yellow" title="Не удалось обновить данные">
                  <Stack gap="xs" align="flex-start">
                    <Text size="sm">{expenses.error.message}</Text>
                    <Button
                      size="xs"
                      variant="light"
                      color="yellow"
                      onClick={() => expenses.refetch()}
                      loading={expenses.isFetching}
                    >
                      Повторить
                    </Button>
                  </Stack>
                </Alert>
              )}
              {/* While a new filter/page loads, the previous result stays visible: dim it so its totals aren't mistaken for the new ones. */}
              <Box pos="relative" mih={stale ? 120 : undefined}>
                <LoadingOverlay visible={stale} overlayProps={{ blur: 1 }} />
                <Stack gap="md" style={{ opacity: stale ? 0.5 : 1, transition: 'opacity 150ms' }}>
                  <Text fw={500}>
                    Итого: {data.total} {pluralize(data.total, ['трата', 'траты', 'трат'])} на {formatMoney(data.sum)}
                  </Text>
                  {data.items.length === 0 ? (
                    <EmptyState
                      py="xl"
                      title="Трат не найдено"
                      description={
                        hasActiveFilters(filters)
                          ? 'Попробуйте изменить фильтры.'
                          : 'Траты появятся, когда члены семьи напишут боту, или добавьте их вручную.'
                      }
                    />
                  ) : (
                    <ExpensesTable items={data.items} onEdit={openEdit} onDelete={openDelete} />
                  )}
                </Stack>
              </Box>
              {totalPages > 1 && (
                <Group justify="center">
                  <Pagination
                    total={totalPages}
                    value={filters.page}
                    onChange={(page) => setFilters({ page })}
                    size="sm"
                  />
                </Group>
              )}
            </Stack>
          ) : null}
        </Paper>
      </Stack>

      <ExpenseFormModal opened={formOpened} expense={editing} onClose={form.close} />
      <DeleteExpenseModal opened={deleteOpened} expense={deleting} onClose={deleteModal.close} />
      <AddUserModal opened={userModalOpened} onClose={userModal.close} />
    </Container>
  );
}
