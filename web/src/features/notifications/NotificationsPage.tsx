import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useSearch } from "@tanstack/react-router";
import { Button } from "@/shared/ui/Button";
import { DataTable } from "@/shared/ui/DataTable";
import { EmptyState } from "@/shared/ui/EmptyState";
import { RowDetailsDrawer } from "@/shared/ui/RowDetailsDrawer";
import { toast } from "@/shared/ui/toast/useToast";
import { describeError } from "@/lib/api/errorMessage";
import { TabList, TabPanel, TabTrigger, Tabs } from "@/shared/ui/Tabs";
import { useTableSearch } from "@/shared/hooks/useTableSearch";
import { useResourceListPage, type BaseListSearch } from "@/shared/hooks/useResourceListPage";
import { ConfirmDeleteDialog } from "@/shared/ui/resourceContextMenu";
import { DocsLink } from "@/shared/ui/DocsLink";
import { ActionDetail } from "./ActionDetail";
import { ActionEditor } from "./ActionEditor";
import { Actions, Notifications } from "./api";
import { actionColumns, notificationColumns, notificationRowDisabled } from "./columns";
import { NotificationDetail } from "./NotificationDetail";
import { NotificationEditor } from "./NotificationEditor";
import type { DeliveryRange } from "./deliveries/types";
import type { Action, Notification } from "./types";
import styles from "./NotificationsPage.module.css";

type PageSearch = BaseListSearch & {
  tab?: "notifications" | "actions";
  /** Row-inspector key (uid) for the Notifications tab — see router.tsx. */
  details?: string | undefined;
  /** Row-inspector key (uid) for the Actions tab. */
  actionDetails?: string | undefined;
  /** Deliveries window (epoch seconds) from the dashboard deep link. */
  from?: number | undefined;
  to?: number | undefined;
};

const PAGE_SIZE = 50;

// Shared by the table and the deep-link fallback drawer so both address rows
// the same way (the URL's `?details=` is a uid). The `?? name` arm only exists
// because `uid` is optional on the type — DataTable needs a non-empty key for
// every row it renders — and a name-keyed row is deliberately NOT resolvable
// by the off-page fallback below (see `looksLikeUid`).
const notificationRowKey = (r: Notification) => r.uid ?? r.name;
const actionRowKey = (r: Action) => r.uid ?? r.name;
// Server uids are UUIDs (`uuid.NewString()` in every DB driver). The off-page
// fallback does `GET /<plugin>/<key>`, which addresses by uid only, so a key
// that came from `rowKey`'s name arm would 404 and be reported as "no longer
// exists" for a row that exists perfectly well — just not on this page.
const UID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const looksLikeUid = (key: string) => UID_RE.test(key);
// The fallback drawer holds exactly one row; prev/next have nowhere to go.
const noNavigate = () => {};

export function NotificationsPage() {
  // useSearch with strict:false returns the validated search params; cast for local type.
  const search = useSearch({ strict: false }) as unknown as PageSearch;

  const tab = search.tab ?? "notifications";
  const page = search.page ?? 1;
  const orderby = search.orderby ?? "name";
  const asc = search.asc ?? true;
  const detailUid = search.uid;
  const [creating, setCreating] = useState(false);

  // Each tab is its own resource with its own selection + delete state, so we
  // wire the shared scaffolding once per resource. Both share the page's URL,
  // so either `updateSearch` is interchangeable — we use the notification one.
  const removeNotification = Notifications.useRemove();
  const removeAction = Actions.useRemove();
  const notif = useResourceListPage<Notification, PageSearch>({
    to: "/web/notifications",
    remove: removeNotification,
    noun: "notification",
  });
  const action = useResourceListPage<Action, PageSearch>({
    to: "/web/notifications",
    remove: removeAction,
    noun: "action",
  });
  const updateSearch = notif.updateSearch;

  // Controlled row inspector: the open row lives in the URL as `?details=`
  // (Notifications) / `?actionDetails=` (Actions), so a dashboard deep link
  // and a pasted URL can land straight on a notification's Deliveries tab.
  // The two tabs list different collections, so one shared key would open the
  // wrong drawer after a tab switch. Closing drops the key AND the deep-linked
  // window, so the next open starts clean.
  const detailsParam = tab === "notifications" ? "details" : "actionDetails";
  const detailsKey = (tab === "notifications" ? search.details : search.actionDetails) ?? null;
  const handleDetailsKeyChange = useCallback(
    (k: string | null) =>
      updateSearch(
        k === null
          ? { [detailsParam]: undefined, from: undefined, to: undefined }
          : { [detailsParam]: k },
      ),
    [updateSearch, detailsParam],
  );
  // A window is only a window when it is well-formed. An unparseable dashboard
  // range arrives as 0/0 (or inverted), which would filter the Deliveries tab
  // down to nothing behind a "— – —" chip — no window at all is the honest
  // reading of a bad one.
  const initialRange = useMemo<DeliveryRange | undefined>(() => {
    const from = search.from;
    const to = search.to;
    if (from === undefined || to === undefined) return undefined;
    if (from <= 0 || to < from) return undefined;
    return { from, to };
  }, [search.from, search.to]);
  const handleRangeClear = useCallback(
    () => updateSearch({ from: undefined, to: undefined }),
    [updateSearch],
  );
  const openEditor = useCallback((uid: string) => updateSearch({ uid }), [updateSearch]);
  const renderNotificationDetails = useCallback(
    (row: Notification) => (
      <NotificationDetail
        row={row}
        onEdit={openEditor}
        initialRange={initialRange}
        onRangeClear={handleRangeClear}
      />
    ),
    [openEditor, initialRange, handleRangeClear],
  );
  const renderActionDetails = useCallback(
    (row: Action) => (
      <ActionDetail row={row} initialRange={initialRange} onRangeClear={handleRangeClear} />
    ),
    [initialRange, handleRangeClear],
  );

  // Each tab carries its own search state. Switching tabs preserves the
  // text in whichever tab the user typed in, but the active list endpoint
  // only sees the filter for the currently-shown tab.
  const notifSearch = useTableSearch({
    collection: "notification",
    placeholder: "name = … AND enabled = true",
    onFilterChange: () => {
      if (page !== 1) updateSearch({ page: 1 });
    },
  });
  const actionSearch = useTableSearch({
    collection: "action",
    placeholder: "action.selected = mail",
    // Distinct URL key so the Notifications-tab and Actions-tab queries don't
    // collide in the address bar (both bars persist independently).
    paramKey: "actionSearch",
    onFilterChange: () => {
      if (page !== 1) updateSearch({ page: 1 });
    },
  });

  const notifList = Notifications.useList({
    offset: (page - 1) * PAGE_SIZE,
    limit: PAGE_SIZE,
    orderby,
    asc,
    ...(notifSearch.q ? { q: notifSearch.q } : {}),
  });
  const actionList = Actions.useList({
    offset: (page - 1) * PAGE_SIZE,
    limit: PAGE_SIZE,
    orderby,
    asc,
    ...(actionSearch.q ? { q: actionSearch.q } : {}),
  });

  const list = tab === "notifications" ? notifList : actionList;

  // Deep links carry a uid, not a page number, so the target row is often not
  // on the page the table happens to be showing (page 2+, an active search, a
  // different sort). `detailsKey` only resolves against the rows in `data`, so
  // without a fallback the dashboard link silently does nothing. Rather than
  // splicing a phantom row into the table's `data` — it would appear in the
  // grid and skew the drawer's "N / M" counter — fetch that one row and render
  // a second, single-row drawer for it.
  const rowsOnPage: { uid?: string; name: string }[] =
    tab === "notifications" ? (notifList.data?.data ?? []) : (actionList.data?.data ?? []);
  const detailsOnPage =
    detailsKey !== null && rowsOnPage.some((r) => (r.uid ?? r.name) === detailsKey);
  const needsFallback =
    detailsKey !== null && looksLikeUid(detailsKey) && list.isSuccess && !detailsOnPage;
  // The other off-page outcome: a key that is not uid-shaped cannot be fetched
  // at all, because `GET /<plugin>/<key>` addresses by uid. There is nothing
  // left to try, so it is the same dead end as a 404 — and it has to be
  // reported as one, or the link leaves a stale `?details=` in the URL with no
  // drawer, no toast and nothing on screen to explain it.
  const unresolvableKey =
    detailsKey !== null && list.isSuccess && !detailsOnPage && !looksLikeUid(detailsKey);
  const fallbackNotification = Notifications.useGet(
    needsFallback && tab === "notifications" ? detailsKey : undefined,
  );
  const fallbackAction = Actions.useGet(
    needsFallback && tab === "actions" ? detailsKey : undefined,
  );
  const fallbackError = tab === "notifications" ? fallbackNotification.error : fallbackAction.error;

  // A uid that 404s is a link to something that has since been deleted. Any
  // other failure (a 403 from a role that lost the read perm, a 500) is a
  // different story but the same dead end: the drawer can never open, so
  // staying silent leaves `?details=` in the URL and every reload retries the
  // same doomed lookup with nothing on screen to explain it. Say what
  // happened in the server's own words and drop the key either way.
  //
  // The latch keys on the uid rather than being a plain boolean: StrictMode
  // runs this effect twice on mount, and the key only clears on the next
  // render, so without it the toast doubles. It is released as soon as the key
  // moves on (including to null, which is what this effect itself does) —
  // latching it for the session would make the SECOND visit to the same dead
  // link silent, which is the dead end all over again.
  const toastedForKey = useRef<string | null>(null);
  useEffect(() => {
    if (toastedForKey.current !== null && toastedForKey.current !== detailsKey) {
      toastedForKey.current = null;
    }
    if (detailsKey === null) return;
    if (!fallbackError && !unresolvableKey) return;
    if (toastedForKey.current === detailsKey) return;
    toastedForKey.current = detailsKey;
    const noun = tab === "notifications" ? "notification" : "action";
    // No error object means the key was never fetchable (non-uid), which reads
    // to the user exactly like a row that has gone: nothing to open.
    toast.error(
      !fallbackError || fallbackError.status === 404
        ? `That ${noun} no longer exists`
        : describeError(fallbackError, `Could not open that ${noun}.`).summary,
    );
    handleDetailsKeyChange(null);
  }, [fallbackError, unresolvableKey, detailsKey, tab, handleDetailsKeyChange]);

  // Toolbar pieces — rendered next to the SearchBar inside DataTable for
  // both tabs so the page chrome matches every other list page. Each tab's
  // `bulkActions` builder is passed straight through to DataTable's own
  // `bulkActions` prop — that's what switches the toolbar chip to its amber
  // "selected" treatment, so we don't build the selected-count text here.
  const toolbarHeader = `${list.data?.meta.total ?? 0} ${tab}`;
  const toolbarActions = (
    <Button size="sm" variant="primary" leadingIcon="plus" onClick={() => setCreating(true)}>
      New
    </Button>
  );

  return (
    <div className={styles.page}>
      {/* Persistent WHEN/HOW mental model — the two-tab distinction was only
          explained in each tab's empty state, so it vanished once a single row
          existed. This strip keeps it available for every future user. */}
      <p className={styles.conceptStrip}>
        <strong>Notifications</strong> decide <em>when</em> an alert should trigger delivery;{" "}
        <strong>Actions</strong> decide <em>how</em> and <em>where</em> it&apos;s delivered.{" "}
        <DocsLink slug="general/notifications" />
      </p>
      <Tabs
        value={tab}
        onValueChange={(v) =>
          // The keys are namespaced per tab, but a switch is still a fresh
          // start: carrying a drawer (or a dashboard window) across it means
          // the other tab opens on state the operator never asked for.
          updateSearch({
            tab: v as "notifications" | "actions",
            page: 1,
            details: undefined,
            actionDetails: undefined,
            from: undefined,
            to: undefined,
          })
        }
      >
        <TabList>
          <TabTrigger value="notifications">Notifications</TabTrigger>
          <TabTrigger value="actions">Actions</TabTrigger>
        </TabList>
        <TabPanel value={tab}>
          {tab === "notifications" ? (
            <DataTable<Notification>
              data={notifList.data?.data ?? []}
              columns={notificationColumns}
              rowKey={notificationRowKey}
              rowDisabled={notificationRowDisabled}
              loading={notifList.isPending}
              contextMenuItems={notif.contextMenuItems}
              rowActions={notif.rowActions}
              selectable
              selectedKeys={notif.selectedKeys}
              onSelectionChange={notif.setSelectedKeys}
              bulkActions={notif.bulkActions}
              search={notifSearch.searchProp}
              toolbarHeader={toolbarHeader}
              toolbar={toolbarActions}
              emptyState={
                <EmptyState
                  icon="file-text"
                  title="No notifications yet"
                  description="Notifications route matching alerts to one or more actions."
                  action={
                    <Button
                      size="md"
                      variant="primary"
                      leadingIcon="plus"
                      onClick={() => setCreating(true)}
                    >
                      New notification
                    </Button>
                  }
                />
              }
              renderDetails={renderNotificationDetails}
              detailsKey={detailsOnPage ? detailsKey : null}
              onDetailsKeyChange={handleDetailsKeyChange}
              serverSort={{
                sortBy: orderby,
                order: asc ? "asc" : "desc",
                onChange: (next) =>
                  updateSearch({
                    orderby: next.sortBy,
                    asc: next.order === "asc",
                    page: 1,
                  }),
              }}
              serverPagination={{
                page,
                pageSize: PAGE_SIZE,
                total: notifList.data?.meta.total ?? 0,
                onChange: (next) => updateSearch({ page: next.page }),
              }}
              onRowOpen={(row) => {
                if (row.uid) updateSearch({ uid: row.uid });
              }}
            />
          ) : (
            <DataTable<Action>
              data={actionList.data?.data ?? []}
              columns={actionColumns}
              rowKey={actionRowKey}
              loading={actionList.isPending}
              contextMenuItems={action.contextMenuItems}
              rowActions={action.rowActions}
              selectable
              selectedKeys={action.selectedKeys}
              onSelectionChange={action.setSelectedKeys}
              bulkActions={action.bulkActions}
              search={actionSearch.searchProp}
              toolbarHeader={toolbarHeader}
              toolbar={toolbarActions}
              emptyState={
                <EmptyState
                  icon="file-text"
                  title="No actions yet"
                  description="Actions describe how to deliver a notification (mail, webhook, …)."
                  action={
                    <Button
                      size="md"
                      variant="primary"
                      leadingIcon="plus"
                      onClick={() => setCreating(true)}
                    >
                      New action
                    </Button>
                  }
                />
              }
              renderDetails={renderActionDetails}
              detailsKey={detailsOnPage ? detailsKey : null}
              onDetailsKeyChange={handleDetailsKeyChange}
              serverSort={{
                sortBy: orderby,
                order: asc ? "asc" : "desc",
                onChange: (next) =>
                  updateSearch({
                    orderby: next.sortBy,
                    asc: next.order === "asc",
                    page: 1,
                  }),
              }}
              serverPagination={{
                page,
                pageSize: PAGE_SIZE,
                total: actionList.data?.meta.total ?? 0,
                onChange: (next) => updateSearch({ page: next.page }),
              }}
              onRowOpen={(row) => {
                if (row.uid) updateSearch({ uid: row.uid });
              }}
            />
          )}
        </TabPanel>
      </Tabs>

      {needsFallback && tab === "notifications" && fallbackNotification.data ? (
        <RowDetailsDrawer<Notification>
          rows={[fallbackNotification.data]}
          rowKey={notificationRowKey}
          activeKey={detailsKey}
          onNavigate={noNavigate}
          onClose={() => handleDetailsKeyChange(null)}
          renderDetails={renderNotificationDetails}
        />
      ) : null}
      {needsFallback && tab === "actions" && fallbackAction.data ? (
        <RowDetailsDrawer<Action>
          rows={[fallbackAction.data]}
          rowKey={actionRowKey}
          activeKey={detailsKey}
          onNavigate={noNavigate}
          onClose={() => handleDetailsKeyChange(null)}
          renderDetails={renderActionDetails}
        />
      ) : null}
      {tab === "notifications" && detailUid !== undefined ? (
        <NotificationEditor uid={detailUid} onClose={() => updateSearch({ uid: undefined })} />
      ) : null}
      {tab === "actions" && detailUid !== undefined ? (
        <ActionEditor uid={detailUid} onClose={() => updateSearch({ uid: undefined })} />
      ) : null}
      {creating && tab === "notifications" ? (
        <NotificationEditor uid={undefined} onClose={() => setCreating(false)} />
      ) : null}
      {creating && tab === "actions" ? (
        <ActionEditor uid={undefined} onClose={() => setCreating(false)} />
      ) : null}
      <ConfirmDeleteDialog
        state={notif.confirmDelete.state}
        onCancel={notif.confirmDelete.cancel}
        onConfirm={() => void notif.confirmDelete.confirm()}
      />
      <ConfirmDeleteDialog
        state={action.confirmDelete.state}
        onCancel={action.confirmDelete.cancel}
        onConfirm={() => void action.confirmDelete.confirm()}
      />
    </div>
  );
}
