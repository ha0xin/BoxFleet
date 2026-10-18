import { useQuery } from "@tanstack/react-query";
import { Banner, Button, Dialog, Table } from "@cloudflare/kumo";

import type { AdminNode, NodeAddress } from "../types";
import { getNodeNetwork, useAdminApi } from "@/admin/api";
import { adminKeys, refreshIntervals } from "@/admin/query";
import { formatRelativeTime } from "./operations-common";

export function addressState(address: NodeAddress): string {
  const states = [
    !address.interface_up && "Interface down",
    address.dad_failed && "DAD failed",
    address.tentative && "Tentative",
    address.deprecated && "Deprecated",
    address.temporary && "Temporary"
  ].filter(Boolean);
  return states.join(", ") || (address.flags_known ? "Configured" : "Configured · flags unavailable");
}

export function NodeAddressesDialog({ node, onClose }: { node: AdminNode; onClose: () => void }) {
  const { request } = useAdminApi();
  const query = useQuery({
    queryKey: adminKeys.nodeAddresses(node.id),
    queryFn: () => getNodeNetwork(request, node.id),
    staleTime: 0,
    refetchInterval: refreshIntervals.live
  });
  const network = query.data;
  const supported = node.capabilities?.includes("network.inventory.v1");
  return (
    <Dialog.Root open onOpenChange={(open) => { if (!open) onClose(); }}>
      <Dialog size="xl" className="max-h-[calc(100dvh-2rem)] overflow-y-auto p-6">
        <Dialog.Title className="text-xl font-semibold">Addresses · {node.name}</Dialog.Title>
        <Dialog.Description className="mb-4 text-kumo-subtle">
          Addresses reported by the agent and public sources observed by the server.
        </Dialog.Description>
        <div className="flex flex-col gap-4">
          <div>
            <h3 className="mb-1 font-medium">Published entry</h3>
            <p className="break-all text-sm text-kumo-subtle">{node.hosts?.map((host) => host.host).join(", ") || node.public_host}</p>
          </div>
          {query.isPending ? <p role="status">Loading addresses…</p> : null}
          {query.isError ? <Banner variant="error">{query.error.message}</Banner> : null}
          {network ? <>
            <div>
              <h3 className="mb-1 font-medium">Observed public source</h3>
              {network.observed_ips.length === 0 ? <p className="text-sm text-kumo-subtle">No public source observed yet.</p> : (
                <ul className="flex flex-col gap-2 text-sm">
                  {network.observed_ips.map((ip) => <li key={`${ip.source}/${ip.address}`} className="break-all">
                    <span className="font-medium">IPv{ip.family} · </span><span className="font-mono">{ip.address}</span>
                    <span className="text-kumo-subtle"> · {formatRelativeTime(ip.observed_at)}{ip.stale ? " · Stale" : ""} · {ip.source === "cloudflare" ? "via Cloudflare" : "direct"}</span>
                  </li>)}
                </ul>
              )}
            </div>
            <div>
              <h3 className="mb-1 font-medium">Local addresses</h3>
              {network.revision === 0 ? <p className="text-sm text-kumo-subtle">{supported ? "Waiting for the first address report." : "Upgrade the agent to discover local addresses."}</p> : null}
              {network.error ? <Banner variant="error">Collection failed: {network.error}. Showing the last successful inventory.</Banner> : null}
              {network.received_at ? <p className="mb-2 text-sm text-kumo-subtle">Last received {formatRelativeTime(network.received_at)}{network.stale ? " · Stale" : ""}</p> : null}
              <div className="max-w-full overflow-x-auto">
                <Table className="min-w-[640px]">
                  <Table.Header><Table.Row>
                    <Table.Head>Address</Table.Head><Table.Head>Interface</Table.Head><Table.Head>State</Table.Head><Table.Head>Scope</Table.Head>
                  </Table.Row></Table.Header>
                  <Table.Body>
                    {network.addresses.map((address) => <Table.Row key={`${address.interface_index}/${address.address}/${address.prefix_length}`}>
                      <Table.Cell><span className="whitespace-nowrap font-mono text-xs">{address.address}/{address.prefix_length}</span></Table.Cell>
                      <Table.Cell>{address.interface}</Table.Cell>
                      <Table.Cell>{addressState(address)}</Table.Cell>
                      <Table.Cell>{address.scope}</Table.Cell>
                    </Table.Row>)}
                    {network.addresses.length === 0 ? <Table.Row><Table.Cell colSpan={4}>No local addresses reported.</Table.Cell></Table.Row> : null}
                  </Table.Body>
                </Table>
              </div>
            </div>
          </> : null}
          <div className="flex justify-end"><Button onClick={onClose}>Close</Button></div>
        </div>
      </Dialog>
    </Dialog.Root>
  );
}
