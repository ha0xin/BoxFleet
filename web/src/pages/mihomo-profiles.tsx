import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  ArrowDownIcon,
  ArrowUpIcon,
  ArrowsClockwiseIcon,
  BracketsCurlyIcon,
  CheckCircleIcon,
  CodeIcon,
  CopyIcon,
  DotsThreeIcon,
  FloppyDiskIcon,
  FunnelIcon,
  LinkSimpleIcon,
  PencilSimpleIcon,
  PlusIcon,
  RocketLaunchIcon,
  SortAscendingIcon,
  SortDescendingIcon,
  TrashIcon
} from "@phosphor-icons/react";
import { Banner, Button, Dialog, DropdownMenu, Input, Loader, Pagination, Select, Surface, Switch, Table, Tabs, Text } from "@cloudflare/kumo";

import { useAdminMutation } from "@/admin/use-admin-mutation";
import { MihomoCodeEditor } from "@/components/mihomo-code-editor";
import type { AdminRequest } from "@/publish/publish-status";
import { formatRelativeTime, PageHeader, PageTopBar, rowLinkClassName } from "./operations-common";
import type {
  AdminUser,
  MihomoPreview,
  MihomoProfile,
  MihomoProfileDocument,
  MihomoProfileRevision,
  MihomoProfileSubscription,
  MihomoRewrite,
  MihomoRewriteTemplate
} from "@/types";

type PageTab = "configurations" | "rewrites";
type SortDirection = "asc" | "desc";
type ConfigurationFilter = "all" | "published" | "unpublished";
type ConfigurationSort = "name" | "user" | "processors" | "published" | "updated";
type TemplateFilter = "all" | "yaml" | "javascript";
type TemplateSort = "name" | "type" | "availability" | "updated";

const emptyDocument = (): MihomoProfileDocument => ({ rewrites: [] });

function copyDocument(document: MihomoProfileDocument): MihomoProfileDocument {
  return { rewrites: (document.rewrites ?? []).map((rewrite) => ({ ...rewrite })) };
}

function rewriteID() {
  return `rw_${crypto.randomUUID().replaceAll("-", "")}`;
}

function fromTemplate(template: MihomoRewriteTemplate): MihomoRewrite {
  return {
    id: rewriteID(),
    template_id: template.id,
    name: template.name,
    kind: template.kind,
    content: template.content,
    enabled: true
  };
}

function customRewrite(kind: MihomoRewrite["kind"]): MihomoRewrite {
  return {
    id: rewriteID(),
    name: kind === "yaml" ? "Custom YAML" : "Custom JavaScript",
    kind,
    content: kind === "yaml"
      ? "+rules:\n  - DOMAIN-SUFFIX,example.com,DIRECT\n"
      : "function main(config) {\n  // Change config and return it synchronously.\n  return config;\n}\n",
    enabled: true
  };
}

export function MihomoProfilesPage({ request }: { request: AdminRequest }) {
  const [tab, setTab] = useState<PageTab>("configurations");
  const [createOpen, setCreateOpen] = useState(false);
  const [editorProfile, setEditorProfile] = useState<MihomoProfile | null>(null);
  const [subscriptionProfile, setSubscriptionProfile] = useState<MihomoProfile | null>(null);
  const [templateDialog, setTemplateDialog] = useState<MihomoRewriteTemplate | "new" | null>(null);

  const profilesQuery = useQuery({
    queryKey: ["admin", "mihomo-profiles"],
    queryFn: () => request<MihomoProfile[]>("/api/admin/mihomo/profiles")
  });
  const usersQuery = useQuery({
    queryKey: ["admin", "users", "mihomo-configurations"],
    queryFn: () => request<AdminUser[]>("/api/admin/users")
  });
  const templatesQuery = useQuery({
    queryKey: ["admin", "mihomo-rewrite-templates"],
    queryFn: () => request<MihomoRewriteTemplate[]>("/api/admin/mihomo/rewrite-templates")
  });

  const profiles = profilesQuery.data ?? [];
  const templates = templatesQuery.data ?? [];
  const users = (usersQuery.data ?? []).filter((user) => !user.deleted_at);

  useEffect(() => {
    if (!editorProfile) return;
    const current = profiles.find((profile) => profile.id === editorProfile.id);
    if (current && current.updated_at !== editorProfile.updated_at) setEditorProfile(current);
  }, [profiles, editorProfile]);

  return (
    <div className="flex min-h-full flex-col bg-kumo-canvas">
      <PageTopBar current="Mihomo Profiles" />
      <div className="relative z-[19] min-h-21 bg-kumo-canvas pb-2">
        <div className="mx-auto w-full max-w-[1400px] px-6 pt-3 pb-1 md:px-8 lg:px-10" />
      </div>
      <main className="w-full grow bg-kumo-canvas">
        <PageHeader
          title="Mihomo Profiles"
          description="Build complete Mihomo subscriptions from inline proxies and ordered rewrite pipelines."
          actions={
            <Button variant="primary" icon={PlusIcon} onClick={() => tab === "configurations" ? setCreateOpen(true) : setTemplateDialog("new")}>
              {tab === "configurations" ? "New configuration" : "New rewrite"}
            </Button>
          }
        />
        <div className="mx-auto flex w-full max-w-[1400px] flex-col gap-4 px-6 pb-8 md:px-8 lg:px-10">
          <div className="border-b border-kumo-line">
            <Tabs
              variant="underline"
              value={tab}
              onValueChange={(value) => setTab(value as PageTab)}
              tabs={[
                { value: "configurations", label: "Mihomo configurations" },
                { value: "rewrites", label: "Rewrite templates" }
              ]}
            />
          </div>
          {tab === "configurations" ? (
            <ConfigurationInventory profiles={profiles} loading={profilesQuery.isLoading} error={profilesQuery.error} onEdit={setEditorProfile} onSubscription={setSubscriptionProfile} />
          ) : (
            <TemplateInventory templates={templates} loading={templatesQuery.isLoading} error={templatesQuery.error} onOpen={setTemplateDialog} />
          )}
        </div>
      </main>

      {createOpen ? (
        <CreateConfigurationDialog
          request={request}
          users={users}
          templates={templates}
          onClose={() => setCreateOpen(false)}
          onCreated={(profile) => { setCreateOpen(false); setEditorProfile(profile); }}
        />
      ) : null}
      {editorProfile ? (
        <ConfigurationEditorDialog
          request={request}
          profile={editorProfile}
          templates={templates}
          onClose={() => setEditorProfile(null)}
        />
      ) : null}
      {subscriptionProfile ? (
        <SubscriptionLinkDialog request={request} profile={subscriptionProfile} onClose={() => setSubscriptionProfile(null)} />
      ) : null}
      {templateDialog ? (
        <RewriteTemplateDialog request={request} template={templateDialog} onClose={() => setTemplateDialog(null)} />
      ) : null}
    </div>
  );
}

function SortHead({ label, column, sort, direction, setSort }: {
  label: string;
  column: string;
  sort: string;
  direction: SortDirection;
  setSort: (column: string) => void;
}) {
  const active = sort === column;
  const Icon = active && direction === "desc" ? SortDescendingIcon : SortAscendingIcon;
  return (
    <Table.Head>
      <button type="button" className="inline-flex items-center gap-1 text-left font-medium text-kumo-default hover:text-kumo-strong" onClick={() => setSort(column)}>
        {label}
        <Icon className={`size-3.5 ${active ? "text-kumo-default" : "text-kumo-subtle"}`} />
      </button>
    </Table.Head>
  );
}

function compareValue(left: string | number, right: string | number, direction: SortDirection) {
  return String(left).localeCompare(String(right), undefined, { numeric: true }) * (direction === "desc" ? -1 : 1);
}

function ConfigurationInventory({ profiles, loading, error, onEdit, onSubscription }: {
  profiles: MihomoProfile[];
  loading: boolean;
  error: unknown;
  onEdit: (profile: MihomoProfile) => void;
  onSubscription: (profile: MihomoProfile) => void;
}) {
  const [page, setPage] = useState(1);
  const [perPage, setPerPage] = useState(10);
  const [searchInput, setSearchInput] = useState("");
  const [search, setSearch] = useState("");
  const [filter, setFilter] = useState<ConfigurationFilter>("all");
  const [sort, setSortValue] = useState<ConfigurationSort>("updated");
  const [direction, setDirection] = useState<SortDirection>("desc");
  const rows = useMemo(() => profiles.filter((profile) => {
    const query = search.toLocaleLowerCase();
    const matchesSearch = !query || `${profile.name} ${profile.proxy_user_name}`.toLocaleLowerCase().includes(query);
    const matchesFilter = filter === "all" || (filter === "published" ? profile.published_version > 0 : profile.published_version === 0);
    return matchesSearch && matchesFilter;
  }).sort((left, right) => {
    const values: Record<ConfigurationSort, [string | number, string | number]> = {
      name: [left.name, right.name], user: [left.proxy_user_name, right.proxy_user_name],
      processors: [left.draft.rewrites.length, right.draft.rewrites.length],
      published: [left.published_version, right.published_version], updated: [left.updated_at, right.updated_at]
    };
    return compareValue(values[sort][0], values[sort][1], direction);
  }), [profiles, search, filter, sort, direction]);
  const total = rows.length;
  const offset = (page - 1) * perPage;
  const visible = rows.slice(offset, offset + perPage);
  function setSort(column: string) {
    const next = column as ConfigurationSort;
    if (sort === next) setDirection((current) => current === "asc" ? "desc" : "asc");
    else { setSortValue(next); setDirection(next === "updated" ? "desc" : "asc"); }
    setPage(1);
  }
  return (
    <section className="flex flex-col gap-3">
      <InventoryHeading title="Configuration inventory" total={total} offset={offset} perPage={perPage} />
      <InventoryTools
        searchInput={searchInput}
        placeholder="Search by configuration or user"
        ariaLabel="Search Mihomo configurations"
        setSearchInput={setSearchInput}
        submit={() => { setSearch(searchInput.trim()); setPage(1); }}
        filter={filter}
        setFilter={(value) => { setFilter(value as ConfigurationFilter); setPage(1); }}
        options={[{ value: "all", label: "All" }, { value: "published", label: "Published" }, { value: "unpublished", label: "Unpublished" }]}
      />
      <div className="overflow-hidden rounded-lg border border-kumo-line bg-kumo-base">
        <div className="overflow-x-auto">
          <Table>
            <Table.Header variant="compact"><Table.Row>
              <SortHead label="Configuration" column="name" sort={sort} direction={direction} setSort={setSort} />
              <SortHead label="User" column="user" sort={sort} direction={direction} setSort={setSort} />
              <SortHead label="Processors" column="processors" sort={sort} direction={direction} setSort={setSort} />
              <SortHead label="Status" column="published" sort={sort} direction={direction} setSort={setSort} />
              <SortHead label="Updated" column="updated" sort={sort} direction={direction} setSort={setSort} />
              <Table.Head className="text-right"><span className="sr-only">Actions</span></Table.Head>
            </Table.Row></Table.Header>
            <Table.Body>
              {error ? <TableEmpty colSpan={6}>{error instanceof Error ? error.message : "Request failed."}</TableEmpty> : loading ? <TableLoading colSpan={6} /> : visible.length ? visible.map((profile) => {
                const enabled = profile.draft.rewrites.filter((rewrite) => rewrite.enabled).length;
                const published = profile.published_version > 0;
                return <Table.Row key={profile.id}>
                  <Table.Cell><div className="flex min-w-52 items-center gap-2"><BracketsCurlyIcon className="size-4 shrink-0 text-kumo-subtle" /><button type="button" className={rowLinkClassName} onClick={() => onEdit(profile)}>{profile.name}</button></div></Table.Cell>
                  <Table.Cell><span className="whitespace-nowrap text-kumo-subtle">{profile.proxy_user_name}</span></Table.Cell>
                  <Table.Cell><span className="whitespace-nowrap text-kumo-subtle">{enabled} of {profile.draft.rewrites.length} enabled</span></Table.Cell>
                  <Table.Cell><span className={`inline-flex items-center gap-1.5 whitespace-nowrap text-sm font-medium ${published ? "text-kumo-success" : "text-kumo-subtle"}`}>{published ? <CheckCircleIcon className="size-4" /> : <CodeIcon className="size-4" />}{published ? `Published · v${profile.published_version}` : "Unpublished"}</span></Table.Cell>
                  <Table.Cell><span className="whitespace-nowrap text-kumo-subtle">{formatRelativeTime(profile.updated_at)}</span></Table.Cell>
                  <Table.Cell className="text-right"><ConfigurationRowMenu profile={profile} onEdit={() => onEdit(profile)} onSubscription={() => onSubscription(profile)} /></Table.Cell>
                </Table.Row>;
              }) : <TableEmpty colSpan={6}>No configurations match this filter.</TableEmpty>}
            </Table.Body>
          </Table>
        </div>
      </div>
      <InventoryPagination page={page} setPage={setPage} perPage={perPage} setPerPage={setPerPage} total={total} />
    </section>
  );
}

function TemplateInventory({ templates, loading, error, onOpen }: {
  templates: MihomoRewriteTemplate[];
  loading: boolean;
  error: unknown;
  onOpen: (template: MihomoRewriteTemplate) => void;
}) {
  const [page, setPage] = useState(1);
  const [perPage, setPerPage] = useState(10);
  const [searchInput, setSearchInput] = useState("");
  const [search, setSearch] = useState("");
  const [filter, setFilter] = useState<TemplateFilter>("all");
  const [sort, setSortValue] = useState<TemplateSort>("name");
  const [direction, setDirection] = useState<SortDirection>("asc");
  const rows = useMemo(() => templates.filter((template) => {
    const matchesSearch = !search || template.name.toLocaleLowerCase().includes(search.toLocaleLowerCase());
    return matchesSearch && (filter === "all" || template.kind === filter);
  }).sort((left, right) => {
    const values: Record<TemplateSort, [string | number, string | number]> = {
      name: [left.name, right.name], type: [left.kind, right.kind], availability: [Number(left.built_in), Number(right.built_in)], updated: [left.updated_at, right.updated_at]
    };
    return compareValue(values[sort][0], values[sort][1], direction);
  }), [templates, search, filter, sort, direction]);
  const total = rows.length;
  const offset = (page - 1) * perPage;
  const visible = rows.slice(offset, offset + perPage);
  function setSort(column: string) {
    const next = column as TemplateSort;
    if (sort === next) setDirection((current) => current === "asc" ? "desc" : "asc");
    else { setSortValue(next); setDirection(next === "updated" ? "desc" : "asc"); }
    setPage(1);
  }
  return (
    <section className="flex flex-col gap-3">
      <InventoryHeading title="Rewrite inventory" total={total} offset={offset} perPage={perPage} />
      <InventoryTools
        searchInput={searchInput}
        placeholder="Search rewrite templates"
        ariaLabel="Search rewrite templates"
        setSearchInput={setSearchInput}
        submit={() => { setSearch(searchInput.trim()); setPage(1); }}
        filter={filter}
        setFilter={(value) => { setFilter(value as TemplateFilter); setPage(1); }}
        options={[{ value: "all", label: "All" }, { value: "yaml", label: "YAML" }, { value: "javascript", label: "JavaScript" }]}
      />
      <div className="overflow-hidden rounded-lg border border-kumo-line bg-kumo-base"><div className="overflow-x-auto">
        <Table>
          <Table.Header variant="compact"><Table.Row>
            <SortHead label="Rewrite" column="name" sort={sort} direction={direction} setSort={setSort} />
            <SortHead label="Type" column="type" sort={sort} direction={direction} setSort={setSort} />
            <SortHead label="Availability" column="availability" sort={sort} direction={direction} setSort={setSort} />
            <SortHead label="Updated" column="updated" sort={sort} direction={direction} setSort={setSort} />
            <Table.Head className="text-right"><span className="sr-only">Actions</span></Table.Head>
          </Table.Row></Table.Header>
          <Table.Body>
            {error ? <TableEmpty colSpan={5}>{error instanceof Error ? error.message : "Request failed."}</TableEmpty> : loading ? <TableLoading colSpan={5} /> : visible.length ? visible.map((template) => (
              <Table.Row key={template.id}>
                <Table.Cell><div className="flex min-w-52 items-center gap-2"><CodeIcon className="size-4 shrink-0 text-kumo-subtle" /><button type="button" className={rowLinkClassName} onClick={() => onOpen(template)}>{template.name}</button></div></Table.Cell>
                <Table.Cell><span className="whitespace-nowrap text-kumo-subtle">{template.kind === "javascript" ? "JavaScript" : "YAML"}</span></Table.Cell>
                <Table.Cell><span className="whitespace-nowrap text-kumo-subtle">{template.built_in ? "Built in · read only" : "Reusable"}</span></Table.Cell>
                <Table.Cell><span className="whitespace-nowrap text-kumo-subtle">{formatRelativeTime(template.updated_at)}</span></Table.Cell>
                <Table.Cell className="text-right"><RowMenu label={`Actions for ${template.name}`} itemLabel={template.built_in ? "Preview" : "Edit"} icon={template.built_in ? <CodeIcon /> : <PencilSimpleIcon />} onSelect={() => onOpen(template)} /></Table.Cell>
              </Table.Row>
            )) : <TableEmpty colSpan={5}>No rewrite templates match this filter.</TableEmpty>}
          </Table.Body>
        </Table>
      </div></div>
      <InventoryPagination page={page} setPage={setPage} perPage={perPage} setPerPage={setPerPage} total={total} />
    </section>
  );
}

function InventoryHeading({ title, total, offset, perPage }: { title: string; total: number; offset: number; perPage: number }) {
  return <div><h2 className="text-base font-semibold text-kumo-default">{title}</h2><p className="text-sm text-kumo-subtle">{total ? `Showing ${offset + 1}-${Math.min(offset + perPage, total)} of ${total}` : "No items"}</p></div>;
}

function InventoryTools({ searchInput, placeholder, ariaLabel, setSearchInput, submit, filter, setFilter, options }: {
  searchInput: string; placeholder: string; ariaLabel: string; setSearchInput: (value: string) => void; submit: () => void;
  filter: string; setFilter: (value: string) => void; options: Array<{ value: string; label: string }>;
}) {
  return <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
    <form className="flex min-w-0 flex-1 gap-2" onSubmit={(event) => { event.preventDefault(); submit(); }}>
      <Input placeholder={placeholder} aria-label={ariaLabel} value={searchInput} onChange={(event) => setSearchInput(event.target.value)} className="min-w-0 flex-1" />
      <Button type="submit" variant="secondary">Search</Button>
    </form>
    <DropdownMenu><DropdownMenu.Trigger render={<Button variant="secondary" icon={FunnelIcon}>Filter</Button>} /><DropdownMenu.Content><DropdownMenu.Group><DropdownMenu.Label>Filter</DropdownMenu.Label><DropdownMenu.RadioGroup value={filter} onValueChange={setFilter}>{options.map((option) => <DropdownMenu.RadioItem key={option.value} value={option.value}>{option.label}<DropdownMenu.RadioItemIndicator /></DropdownMenu.RadioItem>)}</DropdownMenu.RadioGroup></DropdownMenu.Group></DropdownMenu.Content></DropdownMenu>
  </div>;
}

function RowMenu({ label, itemLabel, icon, onSelect }: { label: string; itemLabel: string; icon: React.ReactNode; onSelect: () => void }) {
  return <DropdownMenu><DropdownMenu.Trigger render={<Button variant="ghost" size="sm" shape="square" aria-label={label}><DotsThreeIcon className="size-4" /></Button>} /><DropdownMenu.Content><DropdownMenu.Item icon={icon} onClick={onSelect}>{itemLabel}</DropdownMenu.Item></DropdownMenu.Content></DropdownMenu>;
}

function ConfigurationRowMenu({ profile, onEdit, onSubscription }: { profile: MihomoProfile; onEdit: () => void; onSubscription: () => void }) {
  return <DropdownMenu>
    <DropdownMenu.Trigger render={<Button variant="ghost" size="sm" shape="square" aria-label={`Actions for ${profile.name}`}><DotsThreeIcon className="size-4" /></Button>} />
    <DropdownMenu.Content>
      <DropdownMenu.Item icon={<PencilSimpleIcon />} onClick={onEdit}>Edit</DropdownMenu.Item>
      <DropdownMenu.Item icon={<LinkSimpleIcon />} onClick={onSubscription}>Subscription link</DropdownMenu.Item>
    </DropdownMenu.Content>
  </DropdownMenu>;
}

function TableEmpty({ children, colSpan }: { children: string; colSpan: number }) {
  return <Table.Row><Table.Cell colSpan={colSpan}><div className="flex min-h-32 items-center justify-center text-sm text-kumo-subtle">{children}</div></Table.Cell></Table.Row>;
}

function TableLoading({ colSpan }: { colSpan: number }) {
  return <Table.Row><Table.Cell colSpan={colSpan}><div className="flex min-h-32 items-center justify-center"><Loader size={20} /></div></Table.Cell></Table.Row>;
}

function InventoryPagination({ page, setPage, perPage, setPerPage, total }: { page: number; setPage: (page: number) => void; perPage: number; setPerPage: (size: number) => void; total: number }) {
  return <Pagination page={page} setPage={setPage} perPage={perPage} totalCount={total} className="mt-1"><Pagination.Info>{({ pageShowingRange, totalCount }) => <span><strong>{pageShowingRange}</strong> of {totalCount} items</span>}</Pagination.Info><Pagination.Separator /><Pagination.PageSize value={perPage} onChange={(value) => { setPerPage(value); setPage(1); }} options={[10, 25, 50, 100]} label="Items per page:" /><Pagination.Controls controls="simple" /></Pagination>;
}

function CreateConfigurationDialog({
  request, users, templates, onClose, onCreated
}: {
  request: AdminRequest;
  users: AdminUser[];
  templates: MihomoRewriteTemplate[];
  onClose: () => void;
  onCreated: (profile: MihomoProfile) => void;
}) {
  const basic = templates.find((template) => template.built_in) ?? templates[0];
  const [step, setStep] = useState(1);
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [user, setUser] = useState(users[0]?.name ?? "");
  const [document, setDocument] = useState<MihomoProfileDocument>(() => ({ rewrites: basic ? [fromTemplate(basic)] : [] }));
  const create = useAdminMutation<unknown, MihomoProfile>(
    request,
    (req) => req("/api/admin/mihomo/profiles", {
      method: "POST",
      body: JSON.stringify({ name: name.trim(), description: description.trim(), user, draft: document })
    }),
    { onSuccess: onCreated }
  );
  const userItems = useMemo(() => Object.fromEntries(users.map((item) => [item.name, item.display_name || item.name])), [users]);

  return (
    <Dialog.Root open onOpenChange={(open) => open ? undefined : onClose()}>
      <Dialog size="xl" className="max-h-[calc(100vh-2rem)] overflow-y-auto p-6">
        <Dialog.Title className="text-xl font-semibold text-kumo-default">New Mihomo configuration</Dialog.Title>
        <Dialog.Description className="mb-4 text-kumo-subtle">Step {step} of 2 · {step === 1 ? "Choose the proxy source" : "Build the initial rewrite pipeline"}</Dialog.Description>
        {create.error ? <Banner variant="error" title={create.error.message} /> : null}
        {step === 1 ? (
          <div className="grid gap-4 sm:grid-cols-2">
            <Input label="Configuration name" value={name} onChange={(event) => setName(event.target.value)} />
            <Select label="Proxies from user" value={user} items={userItems} onValueChange={(value) => value && setUser(value)} />
            <div className="sm:col-span-2">
              <Input label="Description" value={description} onChange={(event) => setDescription(event.target.value)} />
            </div>
            <div className="sm:col-span-2 rounded-lg border border-kumo-line bg-kumo-tint p-3 text-sm text-kumo-subtle">
              This user can own multiple configurations. Each one receives its own published revision and subscription URL.
            </div>
          </div>
        ) : (
          <PipelineEditor document={document} setDocument={setDocument} templates={templates} />
        )}
        <div className="mt-5 flex justify-between gap-2">
          <Button variant="secondary" onClick={step === 1 ? onClose : () => setStep(1)}>{step === 1 ? "Cancel" : "Back"}</Button>
          {step === 1 ? (
            <Button disabled={!name.trim() || !user} onClick={() => setStep(2)}>Continue</Button>
          ) : (
            <Button loading={create.isPending} disabled={!document.rewrites.length} onClick={() => create.mutate({})}>Create configuration</Button>
          )}
        </div>
      </Dialog>
    </Dialog.Root>
  );
}

function SubscriptionLinkDialog({ request, profile, onClose }: {
  request: AdminRequest;
  profile: MihomoProfile;
  onClose: () => void;
}) {
  const [copied, setCopied] = useState(false);
  const [confirmation, setConfirmation] = useState<"rotate" | "revoke" | null>(null);
  const subscriptionQuery = useQuery({
    queryKey: ["admin", "mihomo-profile-subscription", profile.id],
    queryFn: () => request<MihomoProfileSubscription>(`/api/admin/mihomo/profiles/${profile.id}/subscription`)
  });
  const generate = useAdminMutation<undefined, MihomoProfileSubscription>(request, (req) =>
    req(`/api/admin/mihomo/profiles/${profile.id}/subscription`, { method: "POST" })
  );
  const rotate = useAdminMutation<undefined, MihomoProfileSubscription>(request, (req) =>
    req(`/api/admin/mihomo/profiles/${profile.id}/subscription/rotate`, { method: "POST" }),
    { onSuccess: () => setConfirmation(null) }
  );
  const revoke = useAdminMutation<undefined, MihomoProfileSubscription>(request, (req) =>
    req(`/api/admin/mihomo/profiles/${profile.id}/subscription`, { method: "DELETE" }),
    { onSuccess: () => setConfirmation(null) }
  );
  const subscription = subscriptionQuery.data;
  const error = subscriptionQuery.error ?? generate.error ?? rotate.error ?? revoke.error;
  return (
    <Dialog.Root open onOpenChange={(open) => open ? undefined : onClose()}>
      <Dialog size="sm" className="p-6">
        <Dialog.Title className="text-xl font-semibold text-kumo-default">Subscription link</Dialog.Title>
        <Dialog.Description className="mb-4 text-kumo-subtle">{profile.name} · proxies from {profile.proxy_user_name}</Dialog.Description>
        {error ? <Banner variant="error" title={error instanceof Error ? error.message : "Request failed"} /> : null}
        {confirmation ? (
          <div className="mb-4 rounded-lg border border-kumo-line bg-kumo-tint p-4">
            <Text bold>{confirmation === "rotate" ? "Rotate this subscription link?" : "Revoke this subscription link?"}</Text>
            <p className="mt-1 text-sm text-kumo-subtle">The current URL will stop working immediately.</p>
            <div className="mt-3 flex justify-end gap-2">
              <Button size="sm" variant="secondary" onClick={() => setConfirmation(null)}>Cancel</Button>
              <Button
                size="sm"
                variant={confirmation === "revoke" ? "destructive" : "primary"}
                loading={rotate.isPending || revoke.isPending}
                onClick={() => confirmation === "rotate" ? rotate.mutate(undefined) : revoke.mutate(undefined)}
              >
                {confirmation === "rotate" ? "Rotate link" : "Revoke link"}
              </Button>
            </div>
          </div>
        ) : null}
        {subscriptionQuery.isLoading ? (
          <div className="flex min-h-32 items-center justify-center"><Loader size={20} /></div>
        ) : subscription?.active ? (
          <div className="flex flex-col gap-4">
            <div className="flex items-end gap-2">
              <Input label="Mihomo subscription URL" readOnly value={subscription.url} className="min-w-0 flex-1" />
              <Button variant="secondary" icon={<CopyIcon />} onClick={() => { void navigator.clipboard.writeText(subscription.url); setCopied(true); }}>{copied ? "Copied" : "Copy"}</Button>
            </div>
            <dl className="grid gap-2 text-sm text-kumo-subtle sm:grid-cols-2">
              <div><dt className="font-medium text-kumo-default">Created</dt><dd>{formatDate(subscription.created_at)}</dd></div>
              <div><dt className="font-medium text-kumo-default">Last fetched</dt><dd>{subscription.last_used_at ? formatDate(subscription.last_used_at) : "Never"}</dd></div>
            </dl>
            <div className="flex flex-wrap gap-2">
              <Button size="sm" variant="secondary" icon={<ArrowsClockwiseIcon />} onClick={() => setConfirmation("rotate")}>Rotate link</Button>
              <Button size="sm" variant="destructive" icon={<TrashIcon />} onClick={() => setConfirmation("revoke")}>Revoke link</Button>
            </div>
          </div>
        ) : (
          <div className="flex items-center justify-between gap-3 rounded-lg bg-kumo-canvas p-4">
            <p className="text-sm text-kumo-subtle">{profile.published_version ? "No subscription link has been generated." : "Publish this configuration before generating its link."}</p>
            <Button size="sm" icon={<LinkSimpleIcon />} disabled={!profile.published_version} loading={generate.isPending} onClick={() => generate.mutate(undefined)}>Generate link</Button>
          </div>
        )}
        <div className="mt-5 flex justify-end"><Button variant="secondary" onClick={onClose}>Close</Button></div>
      </Dialog>
    </Dialog.Root>
  );
}

function ConfigurationEditorDialog({ request, profile, templates, onClose }: {
  request: AdminRequest;
  profile: MihomoProfile;
  templates: MihomoRewriteTemplate[];
  onClose: () => void;
}) {
  const [document, setDocument] = useState(() => copyDocument(profile.draft));
  const [preview, setPreview] = useState<MihomoPreview | null>(null);
  const dirty = JSON.stringify(document) !== JSON.stringify(profile.draft);
  const save = useAdminMutation<MihomoProfileDocument, MihomoProfile>(request, (req, draft) =>
    req(`/api/admin/mihomo/profiles/${profile.id}`, { method: "PATCH", body: JSON.stringify({ draft }) })
  );
  const runPreview = useAdminMutation<MihomoProfileDocument, MihomoPreview>(request, (req, draft) =>
    req(`/api/admin/mihomo/profiles/${profile.id}/preview`, { method: "POST", body: JSON.stringify({ draft }) }),
    { onSuccess: setPreview }
  );
  const publish = useAdminMutation<MihomoProfileDocument, MihomoProfileRevision>(request, async (req, draft) => {
    await req(`/api/admin/mihomo/profiles/${profile.id}`, { method: "PATCH", body: JSON.stringify({ draft }) });
    return req(`/api/admin/mihomo/profiles/${profile.id}/publish`, { method: "POST", body: JSON.stringify({}) });
  });
  const error = save.error ?? runPreview.error ?? publish.error;
  const busy = save.isPending || runPreview.isPending || publish.isPending;

  return (
    <Dialog.Root open onOpenChange={(open) => open ? undefined : onClose()}>
      <Dialog size="xl" className="max-h-[calc(100vh-2rem)] overflow-y-auto p-6">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <Dialog.Title className="text-xl font-semibold text-kumo-default">{profile.name}</Dialog.Title>
            <Dialog.Description className="text-kumo-subtle">Proxies from {profile.proxy_user_name} · published {profile.published_version ? `v${profile.published_version}` : "never"}</Dialog.Description>
          </div>
          <div className="flex flex-wrap gap-2">
            <Button variant="secondary" icon={<FloppyDiskIcon />} disabled={!dirty || busy} onClick={() => save.mutate(document)}>Save draft</Button>
            <Button variant="secondary" icon={<CodeIcon />} loading={runPreview.isPending} disabled={busy} onClick={() => runPreview.mutate(document)}>Preview config</Button>
            <Button icon={<RocketLaunchIcon />} loading={publish.isPending} disabled={busy} onClick={() => publish.mutate(document)}>Publish</Button>
          </div>
        </div>
        {error ? <div className="mt-4"><Banner variant="error" title={error.message} /></div> : null}
        <div className="mt-5">
          <PipelineEditor document={document} setDocument={(next) => { setDocument(next); setPreview(null); }} templates={templates} />
        </div>

        {preview ? (
          <Surface className="mt-5 rounded-lg p-4">
            <div className="mb-3 flex items-center justify-between">
              <div><Text bold>Preview config</Text><p className="text-xs text-kumo-subtle">Final YAML after all enabled processors run in order.</p></div>
              <span className="text-xs text-kumo-subtle">{preview.diagnostics.length} diagnostics</span>
            </div>
            {preview.diagnostics.map((diagnostic, index) => (
              <div className="mb-2" key={`${diagnostic.code}-${index}`}><Banner variant={diagnostic.severity === "error" ? "error" : "alert"} title={diagnostic.code}>{diagnostic.message}</Banner></div>
            ))}
            <MihomoCodeEditor kind="yaml" value={preview.yaml} readOnly />
          </Surface>
        ) : null}

        <div className="mt-5 flex justify-end"><Button variant="secondary" onClick={onClose}>Close</Button></div>
      </Dialog>
    </Dialog.Root>
  );
}

function PipelineEditor({ document, setDocument, templates }: {
  document: MihomoProfileDocument;
  setDocument: (document: MihomoProfileDocument) => void;
  templates: MihomoRewriteTemplate[];
}) {
  const [selectedID, setSelectedID] = useState(document.rewrites[0]?.id ?? "");
  const [templateID, setTemplateID] = useState("");
  const selected = document.rewrites.find((rewrite) => rewrite.id === selectedID) ?? document.rewrites[0];
  const templateItems = useMemo(() => Object.fromEntries(templates.map((template) => [template.id, template.name])), [templates]);

  useEffect(() => {
    if (!selected && document.rewrites[0]) setSelectedID(document.rewrites[0].id);
  }, [selected, document.rewrites]);

  function update(id: string, patch: Partial<MihomoRewrite>) {
    setDocument({ rewrites: document.rewrites.map((rewrite) => rewrite.id === id ? { ...rewrite, ...patch } : rewrite) });
  }
  function add(item: MihomoRewrite) {
    setDocument({ rewrites: [...document.rewrites, item] });
    setSelectedID(item.id);
  }
  function remove(id: string) {
    const rewrites = document.rewrites.filter((rewrite) => rewrite.id !== id);
    setDocument({ rewrites });
    setSelectedID(rewrites[0]?.id ?? "");
  }
  function move(index: number, delta: number) {
    const target = index + delta;
    if (target < 0 || target >= document.rewrites.length) return;
    const rewrites = [...document.rewrites];
    [rewrites[index], rewrites[target]] = [rewrites[target], rewrites[index]];
    setDocument({ rewrites });
  }

  return (
    <div className="grid min-h-[38rem] gap-4 xl:grid-cols-[20rem_minmax(0,1fr)]">
      <Surface className="rounded-lg p-3">
        <div className="mb-3">
          <Text bold>Processor pipeline</Text>
          <p className="text-xs text-kumo-subtle">Runs from top to bottom. Disabled processors remain saved and published.</p>
        </div>
        <div className="flex flex-col gap-2">
          {document.rewrites.map((rewrite, index) => (
            <button
              type="button"
              key={rewrite.id}
              className={`rounded-md border p-2 text-left ${selected?.id === rewrite.id ? "border-kumo-brand bg-kumo-tint" : "border-kumo-line bg-kumo-base"}`}
              onClick={() => setSelectedID(rewrite.id)}
            >
              <div className="flex items-center justify-between gap-2">
                <span className="truncate text-sm font-medium text-kumo-default">{index + 1}. {rewrite.name}</span>
                <span className="text-xs text-kumo-subtle">{rewrite.kind === "javascript" ? "JS" : "YAML"}</span>
              </div>
              <div className="mt-2 flex items-center justify-between gap-2" onClick={(event) => event.stopPropagation()}>
                <Switch size="sm" label={rewrite.enabled ? "On" : "Off"} checked={rewrite.enabled} onCheckedChange={(enabled) => update(rewrite.id, { enabled })} />
                <div className="flex gap-1">
                  <Button shape="square" size="sm" variant="secondary" aria-label="Move up" disabled={index === 0} onClick={() => move(index, -1)}><ArrowUpIcon /></Button>
                  <Button shape="square" size="sm" variant="secondary" aria-label="Move down" disabled={index === document.rewrites.length - 1} onClick={() => move(index, 1)}><ArrowDownIcon /></Button>
                  <Button shape="square" size="sm" variant="secondary-destructive" aria-label="Remove" onClick={() => remove(rewrite.id)}><TrashIcon /></Button>
                </div>
              </div>
            </button>
          ))}
          {!document.rewrites.length ? <EmptyRow>Add a template or custom processor.</EmptyRow> : null}
        </div>
        <div className="mt-3 flex flex-col gap-2 border-t border-kumo-line pt-3">
          <Select
            label="Add from template"
            placeholder="Choose a template"
            value={templateID}
            items={templateItems}
            onValueChange={(value) => {
              if (!value) return;
              const template = templates.find((item) => item.id === value);
              if (template) add(fromTemplate(template));
              setTemplateID("");
            }}
          />
          <div className="grid grid-cols-2 gap-2">
            <Button size="sm" variant="secondary" icon={<PlusIcon />} onClick={() => add(customRewrite("yaml"))}>Custom YAML</Button>
            <Button size="sm" variant="secondary" icon={<PlusIcon />} onClick={() => add(customRewrite("javascript"))}>Custom JS</Button>
          </div>
        </div>
      </Surface>

      <Surface className="min-w-0 rounded-lg p-4">
        {selected ? (
          <div className="flex flex-col gap-3">
            <div className="flex flex-wrap items-end justify-between gap-3">
              <div className="min-w-0 flex-1">
                {selected.template_id ? (
                  <>
                    <Text bold>{selected.name}</Text>
                    <p className="text-xs text-kumo-subtle">Template snapshot · read-only in this configuration</p>
                  </>
                ) : (
                  <Input label="Processor name" value={selected.name} onChange={(event) => update(selected.id, { name: event.target.value })} />
                )}
              </div>
              <span className="text-xs text-kumo-subtle">{selected.kind === "javascript" ? "JavaScript" : "YAML"}</span>
            </div>
            <MihomoCodeEditor
              key={`${selected.id}:${selected.kind}:${Boolean(selected.template_id)}`}
              kind={selected.kind}
              value={selected.content}
              readOnly={Boolean(selected.template_id)}
              onChange={selected.template_id ? undefined : (content) => update(selected.id, { content })}
            />
          </div>
        ) : (
          <div className="flex h-[34rem] items-center justify-center text-sm text-kumo-subtle">Select or add a processor.</div>
        )}
      </Surface>
    </div>
  );
}

function RewriteTemplateDialog({ request, template, onClose }: {
  request: AdminRequest;
  template: MihomoRewriteTemplate | "new";
  onClose: () => void;
}) {
  const existing = template === "new" ? null : template;
  const readOnly = Boolean(existing?.built_in);
  const [name, setName] = useState(existing?.name ?? "");
  const [description, setDescription] = useState(existing?.description ?? "");
  const [kind, setKind] = useState<MihomoRewrite["kind"]>(existing?.kind ?? "yaml");
  const [content, setContent] = useState(existing?.content ?? "");
  const save = useAdminMutation<undefined, MihomoRewriteTemplate>(request, (req) =>
    req(existing ? `/api/admin/mihomo/rewrite-templates/${existing.id}` : "/api/admin/mihomo/rewrite-templates", {
      method: existing ? "PATCH" : "POST",
      body: JSON.stringify({ name: name.trim(), description: description.trim(), kind, content })
    }),
    { onSuccess: onClose }
  );
  return (
    <Dialog.Root open onOpenChange={(open) => open ? undefined : onClose()}>
      <Dialog size="xl" className="max-h-[calc(100vh-2rem)] overflow-y-auto p-6">
        <Dialog.Title className="text-xl font-semibold text-kumo-default">{readOnly ? "Preview rewrite template" : existing ? "Edit rewrite template" : "New rewrite template"}</Dialog.Title>
        <Dialog.Description className="mb-4 text-kumo-subtle">Templates are reusable globally. Adding one to a configuration stores a read-only snapshot.</Dialog.Description>
        {save.error ? <Banner variant="error" title={save.error.message} /> : null}
        <div className="mb-3 grid gap-3 sm:grid-cols-[1fr_14rem]">
          <Input label="Name" value={name} disabled={readOnly} onChange={(event) => setName(event.target.value)} />
          <Select label="Type" value={kind} disabled={readOnly} items={{ yaml: "YAML", javascript: "JavaScript" }} onValueChange={(value) => value && setKind(value as MihomoRewrite["kind"])} />
          <div className="sm:col-span-2"><Input label="Description" value={description} disabled={readOnly} onChange={(event) => setDescription(event.target.value)} /></div>
        </div>
        <MihomoCodeEditor key={kind} kind={kind} value={content} readOnly={readOnly} onChange={readOnly ? undefined : setContent} />
        <div className="mt-5 flex justify-end gap-2">
          <Button variant="secondary" onClick={onClose}>{readOnly ? "Close" : "Cancel"}</Button>
          {!readOnly ? <Button loading={save.isPending} disabled={!name.trim()} onClick={() => save.mutate(undefined)}>Save template</Button> : null}
        </div>
      </Dialog>
    </Dialog.Root>
  );
}

function EmptyRow({ children }: { children: string }) {
  return <div className="flex min-h-24 items-center justify-center text-sm text-kumo-subtle">{children}</div>;
}

function formatDate(value: string) {
  const time = new Date(value);
  return Number.isNaN(time.getTime()) ? value : time.toLocaleString();
}
