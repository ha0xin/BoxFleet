import { Gauge, HardDrives, Users } from "@phosphor-icons/react";
import { Badge, Grid, LayerCard, Surface, Table, Text } from "@cloudflare/kumo";

import { AppPageHeader } from "@/components/app-page-header";
import type { AdminNode, Overview, TrafficRow } from "../types";
import { formatBytes } from "../utils";

type TrafficByUser = { user: string; upload: number; download: number };

function groupTrafficByUser(rows: TrafficRow[]): TrafficByUser[] {
  const byUser = new Map<string, TrafficByUser>();
  for (const row of rows) {
    const entry = byUser.get(row.user_name) ?? { user: row.user_name, upload: 0, download: 0 };
    if (row.direction.includes("up")) {
      entry.upload += row.billable_bytes;
    } else {
      entry.download += row.billable_bytes;
    }
    byUser.set(row.user_name, entry);
  }
  return [...byUser.values()].sort((a, b) => b.upload + b.download - (a.upload + a.download));
}

function nodeStatus(node: AdminNode): { label: string; variant: "success" | "warning" | "neutral" } {
  if (node.status === "disabled") {
    return { label: "Disabled", variant: "neutral" };
  }
  if (node.apply_status && node.apply_status !== "applied") {
    return { label: "Pending Config", variant: "warning" };
  }
  return { label: "Online", variant: "success" };
}

function Metric({ icon: Icon, label, value }: { icon: typeof Gauge; label: string; value: string }) {
  return (
    <Surface className="rounded-lg p-4">
      <div className="flex items-center gap-2 text-kumo-subtle">
        <Icon size={18} />
        <Text variant="secondary" size="sm">
          {label}
        </Text>
      </div>
      <div className="mt-2">
        <Text variant="heading2" as="p">
          {value}
        </Text>
      </div>
    </Surface>
  );
}

export function OverviewPage({ overview }: { overview: Overview | null }) {
  const nodes = overview?.nodes ?? [];
  const users = overview?.users ?? [];
  const trafficRows = overview?.traffic ?? [];

  const activeNodes = nodes.filter((n) => n.status === "active").length;
  const activeUsers = users.filter((u) => u.status === "active").length;
  const totalTraffic = trafficRows.reduce((sum, r) => sum + r.billable_bytes, 0);
  const trafficUsers = groupTrafficByUser(trafficRows);

  return (
    <AppPageHeader title="Overview" description="Sing-box cluster state at a glance.">
      <Grid variant="4up" gap="base">
        <Metric icon={HardDrives} label="Active Nodes" value={`${activeNodes}/${nodes.length}`} />
        <Metric icon={Users} label="Active Users" value={`${activeUsers}/${users.length}`} />
        <Metric icon={Gauge} label="Billable Traffic" value={formatBytes(totalTraffic)} />
        <Metric icon={Gauge} label="Traffic Users" value={`${trafficUsers.length}`} />
      </Grid>

      <Grid variant="2up" gap="base">
        <LayerCard>
          <LayerCard.Secondary>
            <Text bold as="span">
              Node State
            </Text>
          </LayerCard.Secondary>
          <LayerCard.Primary className="p-0">
            <Table>
              <Table.Header>
                <Table.Row>
                  <Table.Head>Name</Table.Head>
                  <Table.Head>Status</Table.Head>
                  <Table.Head>Host</Table.Head>
                  <Table.Head>Config</Table.Head>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {nodes.slice(0, 6).map((node) => {
                  const status = nodeStatus(node);
                  return (
                    <Table.Row key={node.id}>
                      <Table.Cell>
                        <Text bold>{node.name}</Text>
                      </Table.Cell>
                      <Table.Cell>
                        <Badge appearance="dot" variant={status.variant}>
                          {status.label}
                        </Badge>
                      </Table.Cell>
                      <Table.Cell>
                        <Text variant="mono-secondary">{node.public_host}</Text>
                      </Table.Cell>
                      <Table.Cell>
                        <Text variant="secondary" size="sm">
                          {node.current_version ?? "—"} / {node.target_version ?? "—"}
                        </Text>
                      </Table.Cell>
                    </Table.Row>
                  );
                })}
              </Table.Body>
            </Table>
          </LayerCard.Primary>
        </LayerCard>

        <LayerCard>
          <LayerCard.Secondary>
            <Text bold as="span">
              User Traffic
            </Text>
          </LayerCard.Secondary>
          <LayerCard.Primary className="p-0">
            <Table>
              <Table.Header>
                <Table.Row>
                  <Table.Head>User</Table.Head>
                  <Table.Head>Upload</Table.Head>
                  <Table.Head>Download</Table.Head>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {trafficUsers.slice(0, 8).map((row) => (
                  <Table.Row key={row.user}>
                    <Table.Cell>
                      <Text bold>{row.user}</Text>
                    </Table.Cell>
                    <Table.Cell>
                      <Text variant="secondary" size="sm">
                        {formatBytes(row.upload)}
                      </Text>
                    </Table.Cell>
                    <Table.Cell>
                      <Text variant="secondary" size="sm">
                        {formatBytes(row.download)}
                      </Text>
                    </Table.Cell>
                  </Table.Row>
                ))}
              </Table.Body>
            </Table>
          </LayerCard.Primary>
        </LayerCard>
      </Grid>
    </AppPageHeader>
  );
}
