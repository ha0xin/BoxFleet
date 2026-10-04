package api

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/haoxin/boxfleet/internal/server/db"
)

func TestUpdateCampaignSkipsUnclaimedAndDeletedIdentities(t *testing.T) {
	for _, mode := range []string{"timeout", "deleted-and-replaced"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			store := openAPITestDB(t)
			old, err := store.CreateNode(ctx, "edge", "192.0.2.1", "")
			if err != nil {
				t.Fatal(err)
			}
			detail, _, err := store.CreateNodeUpdateCampaign(ctx, db.CreateNodeUpdateCampaignParams{
				Release: "v1", Components: []string{"sing_box"}, BatchSize: 1, IdempotencyKey: "timeout-case",
				Members: []db.CreateNodeUpdateCampaignMemberParams{{NodeName: old.Name, Kind: "update.sing_box", Payload: json.RawMessage(`{}`)}},
			})
			if err != nil {
				t.Fatal(err)
			}
			controller := newUpdateCampaignController(store, newNodeOperationNotifier())
			detail, err = controller.reconcile(ctx, detail.Campaign.ID)
			if err != nil {
				t.Fatal(err)
			}
			opID := detail.Members[0].OperationID
			op, err := store.GetNodeOperation(ctx, opID)
			if err != nil {
				t.Fatal(err)
			}
			if op.ExpiresAt == "" || op.Status != "queued" {
				t.Fatalf("missing queue deadline: %+v", op)
			}
			if mode == "timeout" {
				requested, _ := time.Parse(time.RFC3339Nano, op.RequestedAt)
				controller.now = func() time.Time { return requested.Add(updateQueueTimeout + time.Second) }
			} else {
				if _, err := store.SoftDeleteNode(ctx, old.ID); err != nil {
					t.Fatal(err)
				}
				fresh, err := store.CreateNode(ctx, old.Name, "192.0.2.2", "")
				if err != nil {
					t.Fatal(err)
				}
				if fresh.ID == old.ID {
					t.Fatal("reused ID")
				}
			}
			detail, err = controller.reconcile(ctx, detail.Campaign.ID)
			if err != nil {
				t.Fatal(err)
			}
			if detail.Campaign.Status != "succeeded" || detail.Members[0].Status != "skipped" || detail.Members[0].Error == "" {
				t.Fatalf("campaign did not finish with explicit skip: %+v", detail)
			}
			op, err = store.GetNodeOperation(ctx, opID)
			if err != nil || op.Status != "cancelled" || op.Attempt != 0 {
				t.Fatalf("unclaimed operation not cancelled: %+v %v", op, err)
			}
			// Subsequent reconciliation must not turn a skipped member back into cancelled.
			detail, err = controller.reconcile(ctx, detail.Campaign.ID)
			if err != nil || detail.Members[0].Status != "skipped" {
				t.Fatalf("lost skip: %+v %v", detail, err)
			}
		})
	}
}
