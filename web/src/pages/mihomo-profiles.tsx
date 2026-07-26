import { useEffect, useMemo, useRef, useState } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "react-router-dom";
import QRCode from "qrcode";
import {
  ArrowDownIcon,
  ArrowUpIcon,
  ArrowsClockwiseIcon,
  BracketsCurlyIcon,
  CodeIcon,
  CopyIcon,
  FunnelIcon,
  LinkSimpleIcon,
  PencilSimpleIcon,
  PlusIcon,
  TrashIcon
} from "@phosphor-icons/react";
import { Badge, Banner, Button, Dialog, DropdownMenu, Input, Loader, Select, Surface, Switch, Table, Tabs, Text } from "@cloudflare/kumo";

import { useAdminMutation } from "@/admin/use-admin-mutation";
import { MihomoCodeEditor } from "@/components/mihomo-code-editor";
import { useAdminApi, type AdminRequest } from "@/admin/api";
import { adminKeys } from "@/admin/query";
import { useSubscription } from "@/admin/use-subscription";
import { AppPageHeader } from "@/components/app-page-header";
import { RowActionsMenu } from "@/components/row-actions-menu";
import { AdminPagination, SortHead, TableCard, TableEmpty, TableError, TableLoading } from "@/components/admin-table";
import { copyText, formatDateTime } from "@/utils";
import { formatRelativeTime, rowLinkClassName } from "./operations-common";
import type {
  AdminUser,
  MihomoPreview,
  MihomoProfile,
  MihomoProfileDocument,
  MihomoProfileSubscription,
  MihomoRewrite,
  MihomoRewriteTemplate
} from "@/types";

type PageTab = "configurations" | "rewrites";
type SortDirection = "asc" | "desc";
type ConfigurationFilter = "all" | "yaml" | "javascript";
type ConfigurationSort = "name" | "user" | "processors" | "updated";
type TemplateFilter = "all" | "yaml" | "javascript";
type TemplateSort = "name" | "type" | "availability" | "updated";

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

export function MihomoProfilesPage() {
  const { request } = useAdminApi();
  const navigate = useNavigate();
  const [tab, setTab] = useState<PageTab>("configurations");
  const [subscriptionProfile, setSubscriptionProfile] = useState<MihomoProfile | null>(null);
  const [templateDialog, setTemplateDialog] = useState<MihomoRewriteTemplate | "new" | null>(null);

  const profilesQuery = useQuery({
    queryKey: adminKeys.mihomoProfiles,
    queryFn: () => request<MihomoProfile[]>("/api/admin/mihomo/profiles")
  });
  const templatesQuery = useQuery({
    queryKey: adminKeys.mihomoTemplates,
    queryFn: () => request<MihomoRewriteTemplate[]>("/api/admin/mihomo/rewrite-templates")
  });

  const profiles = profilesQuery.data ?? [];
  const templates = templatesQuery.data ?? [];
  return (
    <div className="flex min-h-full flex-col bg-kumo-canvas">
      <AppPageHeader
        title="Mihomo Profiles"
        description="Build complete Mihomo subscriptions from inline proxies and ordered rewrite pipelines."
        actions={
          <Button variant="primary" icon={PlusIcon} onClick={() => tab === "configurations" ? navigate("/mihomo-profiles/new") : setTemplateDialog("new")}>
            {tab === "configurations" ? "New configuration" : "New rewrite"}
          </Button>
        }
      />
      <main className="w-full grow bg-kumo-canvas">
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
            <ConfigurationInventory profiles={profiles} loading={profilesQuery.isLoading} error={profilesQuery.error} onEdit={(profile) => navigate(`/mihomo-profiles/${profile.id}/edit`)} onSubscription={setSubscriptionProfile} />
          ) : (
            <TemplateInventory templates={templates} loading={templatesQuery.isLoading} error={templatesQuery.error} onOpen={setTemplateDialog} />
          )}
        </div>
      </main>

      {subscriptionProfile ? (
        <SubscriptionLinkDialog request={request} profile={subscriptionProfile} onClose={() => setSubscriptionProfile(null)} />
      ) : null}
      {templateDialog ? (
        <RewriteTemplateDialog request={request} template={templateDialog} onClose={() => setTemplateDialog(null)} />
      ) : null}
    </div>
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
    const matchesFilter = filter === "all" || profile.document.rewrites.some((rewrite) => rewrite.kind === filter);
    return matchesSearch && matchesFilter;
  }).sort((left, right) => {
    const values: Record<ConfigurationSort, [string | number, string | number]> = {
      name: [left.name, right.name], user: [left.proxy_user_name, right.proxy_user_name],
      processors: [left.document.rewrites.length, right.document.rewrites.length], updated: [left.updated_at, right.updated_at]
    };
    return compareValue(values[sort][0], values[sort][1], direction);
  }), [profiles, search, filter, sort, direction]);
  const total = rows.length;
  const offset = (page - 1) * perPage;
  const visible = rows.slice(offset, offset + perPage);
  function setSort(column: ConfigurationSort) {
    if (sort === column) setDirection((current) => current === "asc" ? "desc" : "asc");
    else { setSortValue(column); setDirection(column === "updated" ? "desc" : "asc"); }
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
        options={[{ value: "all", label: "All" }, { value: "yaml", label: "YAML" }, { value: "javascript", label: "JavaScript" }]}
      />
      <TableCard>
        <Table>
          <Table.Header variant="compact"><Table.Row>
            <SortHead label="Configuration" column="name" sort={sort} direction={direction} setSort={setSort} />
            <SortHead label="User" column="user" sort={sort} direction={direction} setSort={setSort} />
            <SortHead label="Processors" column="processors" sort={sort} direction={direction} setSort={setSort} />
            <SortHead label="Updated" column="updated" sort={sort} direction={direction} setSort={setSort} />
            <Table.Head className="text-right"><span className="sr-only">Actions</span></Table.Head>
          </Table.Row></Table.Header>
          <Table.Body>
            {error ? <TableError colSpan={5}>{error instanceof Error ? error.message : "Request failed."}</TableError> : loading ? <TableLoading colSpan={5} /> : visible.length ? visible.map((profile) => {
              const enabled = profile.document.rewrites.filter((rewrite) => rewrite.enabled).length;
              return <Table.Row key={profile.id}>
                <Table.Cell><div className="flex min-w-52 items-center gap-2"><BracketsCurlyIcon className="size-4 shrink-0 text-kumo-subtle" /><Link to={`/mihomo-profiles/${profile.id}/edit`} className={rowLinkClassName}>{profile.name}</Link></div></Table.Cell>
                <Table.Cell><span className="whitespace-nowrap text-kumo-subtle">{profile.proxy_user_name}</span></Table.Cell>
                <Table.Cell><span className="whitespace-nowrap text-kumo-subtle">{enabled} of {profile.document.rewrites.length} enabled</span></Table.Cell>
                <Table.Cell><span className="whitespace-nowrap text-kumo-subtle">{formatRelativeTime(profile.updated_at)}</span></Table.Cell>
                <Table.Cell className="text-right">
                  <RowActionsMenu label={`Actions for ${profile.name}`}>
                    <DropdownMenu.Item icon={PencilSimpleIcon} onClick={() => onEdit(profile)}>Edit</DropdownMenu.Item>
                    <DropdownMenu.Item icon={LinkSimpleIcon} onClick={() => onSubscription(profile)}>Subscription link</DropdownMenu.Item>
                  </RowActionsMenu>
                </Table.Cell>
              </Table.Row>;
            }) : <TableEmpty colSpan={5}>No configurations match this filter.</TableEmpty>}
          </Table.Body>
        </Table>
      </TableCard>
      <AdminPagination page={page} setPage={setPage} perPage={perPage} setPerPage={setPerPage} total={total} />
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
  function setSort(column: TemplateSort) {
    if (sort === column) setDirection((current) => current === "asc" ? "desc" : "asc");
    else { setSortValue(column); setDirection(column === "updated" ? "desc" : "asc"); }
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
      <TableCard>
        <Table>
          <Table.Header variant="compact"><Table.Row>
            <SortHead label="Rewrite" column="name" sort={sort} direction={direction} setSort={setSort} />
            <SortHead label="Type" column="type" sort={sort} direction={direction} setSort={setSort} />
            <SortHead label="Availability" column="availability" sort={sort} direction={direction} setSort={setSort} />
            <SortHead label="Updated" column="updated" sort={sort} direction={direction} setSort={setSort} />
            <Table.Head className="text-right"><span className="sr-only">Actions</span></Table.Head>
          </Table.Row></Table.Header>
          <Table.Body>
            {error ? <TableError colSpan={5}>{error instanceof Error ? error.message : "Request failed."}</TableError> : loading ? <TableLoading colSpan={5} /> : visible.length ? visible.map((template) => (
              <Table.Row key={template.id}>
                <Table.Cell><div className="flex min-w-52 items-center gap-2"><CodeIcon className="size-4 shrink-0 text-kumo-subtle" /><button type="button" className={rowLinkClassName} onClick={() => onOpen(template)}>{template.name}</button></div></Table.Cell>
                <Table.Cell><Badge variant="secondary">{template.kind === "javascript" ? "JavaScript" : "YAML"}</Badge></Table.Cell>
                <Table.Cell>
                  <div className="flex items-center gap-2 whitespace-nowrap">
                    <Badge variant="secondary">{template.built_in ? "Built in" : "Reusable"}</Badge>
                    {template.built_in ? <span className="text-xs text-kumo-subtle">read only</span> : null}
                  </div>
                </Table.Cell>
                <Table.Cell><span className="whitespace-nowrap text-kumo-subtle">{formatRelativeTime(template.updated_at)}</span></Table.Cell>
                <Table.Cell className="text-right">
                  <RowActionsMenu label={`Actions for ${template.name}`}>
                    <DropdownMenu.Item icon={template.built_in ? CodeIcon : PencilSimpleIcon} onClick={() => onOpen(template)}>
                      {template.built_in ? "Preview" : "Edit"}
                    </DropdownMenu.Item>
                  </RowActionsMenu>
                </Table.Cell>
              </Table.Row>
            )) : <TableEmpty colSpan={5}>No rewrite templates match this filter.</TableEmpty>}
          </Table.Body>
        </Table>
      </TableCard>
      <AdminPagination page={page} setPage={setPage} perPage={perPage} setPerPage={setPerPage} total={total} />
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

export function MihomoConfigurationPage() {
  const { request } = useAdminApi();
  const navigate = useNavigate();
  const { profile: profileID } = useParams();
  const creating = !profileID;
  const usersQuery = useQuery({
    queryKey: adminKeys.users(false),
    queryFn: () => request<AdminUser[]>("/api/admin/users"),
    enabled: creating
  });
  const templatesQuery = useQuery({
    queryKey: adminKeys.mihomoTemplates,
    queryFn: () => request<MihomoRewriteTemplate[]>("/api/admin/mihomo/rewrite-templates")
  });
  const profileQuery = useQuery({
    queryKey: adminKeys.mihomoProfile(profileID ?? "new"),
    queryFn: () => request<MihomoProfile>(`/api/admin/mihomo/profiles/${profileID}`),
    enabled: !creating
  });
  const error = usersQuery.error ?? templatesQuery.error ?? profileQuery.error;
  const loading = templatesQuery.isLoading || (creating ? usersQuery.isLoading : profileQuery.isLoading);

  if (error) {
    return (
      <ConfigurationPageShell title="Mihomo configuration" description="Unable to load this configuration." actions={<Button variant="secondary" onClick={() => navigate("/mihomo-profiles")}>Back</Button>}>
        <Banner variant="error" title={error instanceof Error ? error.message : "Request failed"} />
      </ConfigurationPageShell>
    );
  }
  if (loading || !templatesQuery.data) {
    return (
      <ConfigurationPageShell title={creating ? "New Mihomo configuration" : "Mihomo configuration"} description="Loading configuration workbench…">
        <div className="flex min-h-72 items-center justify-center"><Loader size={20} /></div>
      </ConfigurationPageShell>
    );
  }
  if (creating) {
    const users = (usersQuery.data ?? []).filter((user) => !user.deleted_at);
    return <NewConfigurationWorkbench request={request} users={users} templates={templatesQuery.data} />;
  }
  if (!profileQuery.data) {
    return (
      <ConfigurationPageShell title="Mihomo configuration" description="The requested configuration was not returned." actions={<Button variant="secondary" onClick={() => navigate("/mihomo-profiles")}>Back</Button>}>
        <Banner variant="error" title="Configuration not found" />
      </ConfigurationPageShell>
    );
  }
  // Keyed on the id only: remounting on every updated_at change would discard
  // pipeline edits made between clicking Save and the refetch landing. The
  // workbench adopts newer server documents itself.
  return <SavedConfigurationWorkbench key={profileQuery.data.id} request={request} profile={profileQuery.data} templates={templatesQuery.data} />;
}

function ConfigurationPageShell({ title, description, actions, children }: {
  title: string;
  description: string;
  actions?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <div className="flex min-h-full flex-col bg-kumo-canvas">
      <AppPageHeader title={title} description={description} actions={actions} />
      <main className="w-full grow bg-kumo-canvas">
        <div className="mx-auto flex w-full max-w-[1400px] flex-col gap-4 px-6 pb-8 md:px-8 lg:px-10">
          {children}
        </div>
      </main>
    </div>
  );
}

function NewConfigurationWorkbench({ request, users, templates }: {
  request: AdminRequest;
  users: AdminUser[];
  templates: MihomoRewriteTemplate[];
}) {
  const navigate = useNavigate();
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
      body: JSON.stringify({ name: name.trim(), description: description.trim(), user, document })
    }),
    { onSuccess: (profile) => navigate(`/mihomo-profiles/${profile.id}/edit`), toastError: false }
  );
  const userItems = useMemo(() => Object.fromEntries(users.map((item) => [item.name, item.display_name || item.name])), [users]);

  return (
    <ConfigurationPageShell
      title="New Mihomo configuration"
      description={`Step ${step} of 2 · ${step === 1 ? "Choose the proxy source" : "Build the initial rewrite pipeline"}`}
      actions={<Button variant="secondary" onClick={() => navigate("/mihomo-profiles")}>Cancel</Button>}
    >
        {create.error ? <Banner variant="error" title={create.error.message} /> : null}
        {step === 1 ? (
          <Surface className="flex max-w-2xl flex-col gap-4 rounded-lg p-5">
            <Input label="Configuration name" value={name} onChange={(event) => setName(event.target.value)} />
            <Select label="Proxies from user" value={user} items={userItems} onValueChange={(value) => value && setUser(value)} />
            <Input label="Description" value={description} onChange={(event) => setDescription(event.target.value)} />
          </Surface>
        ) : (
          <PipelineEditor document={document} setDocument={setDocument} templates={templates} />
        )}
        <div className="sticky bottom-0 z-10 flex justify-between gap-2 border-t border-kumo-line bg-kumo-canvas py-3">
          <Button variant="secondary" onClick={step === 1 ? () => navigate("/mihomo-profiles") : () => setStep(1)}>{step === 1 ? "Cancel" : "Back"}</Button>
          {step === 1 ? (
            <Button disabled={!name.trim() || !user} onClick={() => setStep(2)}>Continue</Button>
          ) : (
            <Button loading={create.isPending} disabled={!document.rewrites.length} onClick={() => create.mutate({})}>Create configuration</Button>
          )}
        </div>
    </ConfigurationPageShell>
  );
}

function SubscriptionQrCode({ value }: { value: string }) {
  const [dataUrl, setDataUrl] = useState("");
  useEffect(() => {
    let cancelled = false;
    setDataUrl("");
    QRCode.toDataURL(value, { margin: 2, width: 224 })
      .then((url) => {
        if (!cancelled) setDataUrl(url);
      })
      .catch(() => {
        if (!cancelled) setDataUrl("");
      });
    return () => {
      cancelled = true;
    };
  }, [value]);
  // The QR image bakes in its own white quiet zone — required for scanning.
  return (
    <div className="size-56 overflow-hidden rounded-md">
      {dataUrl ? (
        <img src={dataUrl} alt="Subscription link QR code" className="size-56" />
      ) : (
        <div className="flex size-full items-center justify-center"><Loader size={20} /></div>
      )}
    </div>
  );
}

function SubscriptionLinkDialog({ request, profile, onClose }: {
  request: AdminRequest;
  profile: MihomoProfile;
  onClose: () => void;
}) {
  const [copied, setCopied] = useState(false);
  const [copyError, setCopyError] = useState("");
  const [confirmation, setConfirmation] = useState<"rotate" | "revoke" | null>(null);
  const endpoint = `/api/admin/mihomo/profiles/${profile.id}/subscription`;
  const { query: subscriptionQuery, generate, rotate, revoke } = useSubscription<MihomoProfileSubscription>(
    request,
    adminKeys.subscription("mihomo-profile", profile.id),
    endpoint,
    () => setConfirmation(null)
  );
  const subscription = subscriptionQuery.data;
  const error = subscriptionQuery.error ?? generate.error;
  const confirmError = confirmation === "rotate" ? rotate.error : confirmation === "revoke" ? revoke.error : null;
  const confirmPending = rotate.isPending || revoke.isPending;

  useEffect(() => {
    if (!copied) return;
    const timer = window.setTimeout(() => setCopied(false), 2000);
    return () => window.clearTimeout(timer);
  }, [copied]);
  useEffect(() => {
    setCopied(false);
  }, [subscription?.url]);

  return (
    <>
      <Dialog.Root open onOpenChange={(open) => open ? undefined : onClose()}>
        <Dialog size="lg" className="max-h-[calc(100dvh-2rem)] overflow-y-auto p-6">
          <Dialog.Title className="text-xl font-semibold text-kumo-default">Subscription link</Dialog.Title>
          <Dialog.Description className="mb-4 text-kumo-subtle">{profile.name} · proxies from {profile.proxy_user_name}</Dialog.Description>
          {error ? <Banner variant="error" title={error instanceof Error ? error.message : "Request failed"} className="mb-4" /> : null}
          {copyError ? <Banner variant="error" title={copyError} className="mb-4" /> : null}
          {subscriptionQuery.isLoading ? (
            <div className="flex min-h-32 items-center justify-center"><Loader size={20} /></div>
          ) : subscription?.active ? (
            <div className="flex flex-col gap-4">
              <div className="flex items-end gap-2">
                <div className="min-w-0 flex-1"><Input label="Mihomo subscription URL" readOnly value={subscription.url} className="w-full" /></div>
                <Button variant="secondary" icon={CopyIcon} onClick={() => {
                  void copyText(subscription.url)
                    .then(() => { setCopyError(""); setCopied(true); })
                    .catch((copyFailure: unknown) => setCopyError(copyFailure instanceof Error ? copyFailure.message : "Unable to copy."));
                }}>{copied ? "Copied" : "Copy"}</Button>
              </div>
              <div className="flex flex-col items-center gap-2 rounded-lg border border-kumo-line bg-kumo-canvas p-4">
                <SubscriptionQrCode value={subscription.url} />
              </div>
              <dl className="grid gap-2 text-sm text-kumo-subtle sm:grid-cols-2">
                <div><dt className="font-medium text-kumo-default">Created</dt><dd>{formatDateTime(subscription.created_at)}</dd></div>
                <div><dt className="font-medium text-kumo-default">Last fetched</dt><dd>{formatDateTime(subscription.last_used_at)}</dd></div>
              </dl>
              <div className="flex flex-wrap gap-2">
                <Button size="sm" variant="secondary" icon={ArrowsClockwiseIcon} onClick={() => setConfirmation("rotate")}>Rotate link</Button>
                <Button size="sm" variant="destructive" icon={TrashIcon} onClick={() => setConfirmation("revoke")}>Revoke link</Button>
              </div>
            </div>
          ) : (
            <div className="flex items-center justify-between gap-3 rounded-lg border border-kumo-line bg-kumo-tint p-4">
              <p className="text-sm text-kumo-subtle">No subscription link has been generated.</p>
              <Button size="sm" icon={LinkSimpleIcon} loading={generate.isPending} onClick={() => generate.mutate()}>Generate link</Button>
            </div>
          )}
          <div className="mt-2 flex justify-end gap-2"><Button variant="secondary" onClick={onClose}>Close</Button></div>
        </Dialog>
      </Dialog.Root>

      <Dialog.Root
        role="alertdialog"
        open={confirmation !== null}
        onOpenChange={(open) => (open || confirmPending ? undefined : setConfirmation(null))}
      >
        <Dialog size="sm" className="max-h-[calc(100dvh-2rem)] overflow-y-auto p-6">
          <Dialog.Title className="text-xl font-semibold text-kumo-default">
            {confirmation === "rotate" ? "Rotate subscription link?" : "Revoke subscription link?"}
          </Dialog.Title>
          <Dialog.Description className="mb-4 text-kumo-subtle">
            The current URL will stop working immediately.
          </Dialog.Description>
          {confirmError ? <Banner variant="error" title={confirmError.message} className="mb-4" /> : null}
          <div className="mt-2 flex justify-end gap-2">
            <Button variant="ghost" disabled={confirmPending} onClick={() => setConfirmation(null)}>Cancel</Button>
            <Button
              variant="destructive"
              loading={confirmPending}
              onClick={() => confirmation === "rotate" ? rotate.mutate() : revoke.mutate()}
            >
              {confirmation === "rotate" ? "Rotate link" : "Revoke link"}
            </Button>
          </div>
        </Dialog>
      </Dialog.Root>
    </>
  );
}

function SavedConfigurationWorkbench({ request, profile, templates }: {
  request: AdminRequest;
  profile: MihomoProfile;
  templates: MihomoRewriteTemplate[];
}) {
  const navigate = useNavigate();
  const [document, setDocument] = useState(() => copyDocument(profile.document));
  const [preview, setPreview] = useState<MihomoPreview | null>(null);
  const documentJSON = JSON.stringify(document);
  const profileJSON = JSON.stringify(profile.document);
  const dirty = documentJSON !== profileJSON;
  // Server document this local copy was forked from. A newer server version is
  // adopted only while nothing was edited locally, so edits made during an
  // in-flight save survive the refetch that follows it.
  const baseline = useRef(profileJSON);
  useEffect(() => {
    if (profileJSON === baseline.current) return;
    if (documentJSON === baseline.current) {
      setDocument(copyDocument(profile.document));
      setPreview(null);
    }
    baseline.current = profileJSON;
  }, [profile.document, profileJSON, documentJSON]);
  const save = useAdminMutation<MihomoProfileDocument, MihomoProfile>(
    request,
    (req, nextDocument) =>
      req(`/api/admin/mihomo/profiles/${profile.id}`, { method: "PATCH", body: JSON.stringify({ document: nextDocument }) }),
    { toastError: false }
  );
  const runPreview = useMutation({
    mutationFn: (nextDocument: MihomoProfileDocument) =>
      request<MihomoPreview>(`/api/admin/mihomo/profiles/${profile.id}/preview`, {
        method: "POST",
        body: JSON.stringify({ document: nextDocument })
      }),
    onSuccess: setPreview
  });
  const error = save.error ?? runPreview.error;
  const busy = save.isPending || runPreview.isPending;

  return (
    <ConfigurationPageShell
      title={profile.name}
      description={`Proxies from ${profile.proxy_user_name} · linked templates always use their latest saved content`}
      actions={
        <>
          <Button variant="secondary" onClick={() => navigate("/mihomo-profiles")}>Back</Button>
          <Button variant="secondary" icon={CodeIcon} loading={runPreview.isPending} disabled={busy} onClick={() => runPreview.mutate(document)}>Preview config</Button>
          <Button loading={save.isPending} disabled={!dirty || busy} onClick={() => save.mutate(document)}>Save</Button>
        </>
      }
    >
        {error ? <Banner variant="error" title={error.message} /> : null}
        <PipelineEditor document={document} setDocument={(next) => { setDocument(next); setPreview(null); }} templates={templates} />

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
    </ConfigurationPageShell>
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
    <div className="grid min-h-[38rem] gap-4 lg:grid-cols-[20rem_minmax(0,1fr)]">
      <Surface className="rounded-lg p-3">
        <div className="mb-3">
          <Text bold>Processor pipeline</Text>
          <p className="text-xs text-kumo-subtle">Runs from top to bottom. Disabled processors remain saved.</p>
        </div>
        <div className="flex flex-col gap-2">
          {document.rewrites.map((rewrite, index) => (
            <div
              key={rewrite.id}
              className={`rounded-md border p-2 ${selected?.id === rewrite.id ? "border-kumo-brand bg-kumo-tint" : "border-kumo-line bg-kumo-base"}`}
            >
              <div className="flex items-center justify-between gap-2">
                <button type="button" className="min-w-0 flex-1 text-left" onClick={() => setSelectedID(rewrite.id)}>
                  <span className="block truncate text-sm font-medium text-kumo-default">{index + 1}. {rewrite.name}</span>
                </button>
                <Badge variant="secondary">{rewrite.kind === "javascript" ? "JS" : "YAML"}</Badge>
              </div>
              <div className="mt-2 flex items-center justify-between gap-2">
                <Switch size="sm" label={rewrite.enabled ? "On" : "Off"} checked={rewrite.enabled} onCheckedChange={(enabled) => update(rewrite.id, { enabled })} />
                <div className="flex gap-1">
                  <Button shape="square" size="sm" variant="secondary" aria-label="Move up" disabled={index === 0} onClick={() => move(index, -1)}><ArrowUpIcon /></Button>
                  <Button shape="square" size="sm" variant="secondary" aria-label="Move down" disabled={index === document.rewrites.length - 1} onClick={() => move(index, 1)}><ArrowDownIcon /></Button>
                  <Button shape="square" size="sm" variant="secondary-destructive" aria-label="Remove" onClick={() => remove(rewrite.id)}><TrashIcon /></Button>
                </div>
              </div>
            </div>
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
            <Button size="sm" variant="secondary" icon={PlusIcon} onClick={() => add(customRewrite("yaml"))}>Custom YAML</Button>
            <Button size="sm" variant="secondary" icon={PlusIcon} onClick={() => add(customRewrite("javascript"))}>Custom JS</Button>
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
                    <p className="text-xs text-kumo-subtle">Linked template · always uses the latest saved version</p>
                  </>
                ) : (
                  <Input label="Processor name" value={selected.name} onChange={(event) => update(selected.id, { name: event.target.value })} />
                )}
              </div>
              <Badge variant="secondary">{selected.kind === "javascript" ? "JavaScript" : "YAML"}</Badge>
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

const templateFormSchema = z.object({
  name: z.string().trim().min(1, "Name is required"),
  description: z.string(),
  kind: z.enum(["yaml", "javascript"]),
  content: z.string()
});

type TemplateFormValues = z.infer<typeof templateFormSchema>;

function RewriteTemplateDialog({ request, template, onClose }: {
  request: AdminRequest;
  template: MihomoRewriteTemplate | "new";
  onClose: () => void;
}) {
  const existing = template === "new" ? null : template;
  const readOnly = Boolean(existing?.built_in);
  const form = useForm<TemplateFormValues>({
    resolver: zodResolver(templateFormSchema),
    defaultValues: {
      name: existing?.name ?? "",
      description: existing?.description ?? "",
      kind: existing?.kind ?? "yaml",
      content: existing?.content ?? ""
    }
  });
  const save = useAdminMutation<TemplateFormValues, MihomoRewriteTemplate>(
    request,
    (req, values) =>
      req(existing ? `/api/admin/mihomo/rewrite-templates/${existing.id}` : "/api/admin/mihomo/rewrite-templates", {
        method: existing ? "PATCH" : "POST",
        body: JSON.stringify({
          name: values.name.trim(),
          description: values.description.trim(),
          kind: values.kind,
          content: values.content
        })
      }),
    { onSuccess: onClose, toastError: false }
  );
  const kind = form.watch("kind");
  const content = form.watch("content");
  const errors = form.formState.errors;
  return (
    <Dialog.Root open onOpenChange={(open) => (open || save.isPending ? undefined : onClose())}>
      <Dialog size="xl" className="max-h-[calc(100dvh-2rem)] overflow-y-auto p-6">
        <Dialog.Title className="text-xl font-semibold text-kumo-default">{readOnly ? "Preview rewrite template" : existing ? "Edit rewrite template" : "New rewrite template"}</Dialog.Title>
        <Dialog.Description className="mb-4 text-kumo-subtle">Templates are reusable globally. Saved changes apply to every linked configuration.</Dialog.Description>
        {save.isError ? <Banner variant="error" title={save.error.message} className="mb-4" /> : null}
        <form className="flex flex-col gap-3" onSubmit={form.handleSubmit((values) => save.mutate(values))}>
          <div className="grid gap-3 sm:grid-cols-[1fr_14rem]">
            <Input label="Name" disabled={readOnly} error={errors.name?.message} {...form.register("name")} />
            <Select
              label="Type"
              value={kind}
              disabled={readOnly}
              items={{ yaml: "YAML", javascript: "JavaScript" }}
              onValueChange={(value) => value && form.setValue("kind", value as TemplateFormValues["kind"])}
            />
            <div className="sm:col-span-2"><Input label="Description" disabled={readOnly} {...form.register("description")} /></div>
          </div>
          <MihomoCodeEditor
            key={kind}
            kind={kind}
            value={content}
            readOnly={readOnly}
            onChange={readOnly ? undefined : (value) => form.setValue("content", value)}
          />
          <div className="mt-2 flex justify-end gap-2">
            <Button type="button" variant={readOnly ? "secondary" : "ghost"} disabled={save.isPending} onClick={onClose}>{readOnly ? "Close" : "Cancel"}</Button>
            {!readOnly ? <Button type="submit" loading={save.isPending}>Save template</Button> : null}
          </div>
        </form>
      </Dialog>
    </Dialog.Root>
  );
}

function EmptyRow({ children }: { children: string }) {
  return <div className="flex min-h-24 items-center justify-center text-sm text-kumo-subtle">{children}</div>;
}
