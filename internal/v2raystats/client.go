package v2raystats

import (
	"context"
	"sort"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/haoxin/boxfleet/internal/v2rayapi"
)

type Stat struct {
	Name  string `json:"name"`
	Value int64  `json:"value"`
}

func Query(ctx context.Context, addr string, patterns []string, reset bool) ([]Stat, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	response, err := v2rayapi.NewStatsServiceClient(conn).QueryStats(ctx, &v2rayapi.QueryStatsRequest{
		Patterns: patterns,
		Reset_:   reset,
	})
	if err != nil {
		return nil, err
	}
	stats := make([]Stat, 0, len(response.GetStat()))
	for _, stat := range response.GetStat() {
		stats = append(stats, Stat{Name: stat.GetName(), Value: stat.GetValue()})
	}
	sort.Slice(stats, func(i, j int) bool {
		return stats[i].Name < stats[j].Name
	})
	return stats, nil
}
