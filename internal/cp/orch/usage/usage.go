// SPDX-License-Identifier: AGPL-3.0-only

// Package usage computes Project Settings > Usage (docs/SPEC.md 9.23): per
// service and in total, vCPU hours, memory GB hours, egress GB (bytes sent
// by containers to non-pod addresses), volume GB months and backup bucket
// bytes for a window, plus what the same use would cost on Railway at its
// list prices.
//
// The prices are the ones published on Railway's pricing page on the date
// below; site/data/railway-prices.json (the landing page) must carry the
// same figures.
package usage

import (
	"context"
	"math"
	"sort"

	"github.com/musakkka/5050cloud/pkg/api"
)

// Prices are Railway's usage list prices (USD).
type Prices struct {
	Date           string  `json:"date"`
	SourceURL      string  `json:"sourceUrl"`
	VCPUMonth      float64 `json:"vcpuMonth"`
	MemGBMonth     float64 `json:"memGbMonth"`
	EgressGB       float64 `json:"egressGb"`
	VolumeGBMonth  float64 `json:"volumeGbMonth"`
	HoursPerMonth  float64 `json:"hoursPerMonth"`
	BucketGBMonth  float64 `json:"bucketGbMonth"`
	BucketIncluded bool    `json:"bucketIncluded"`
}

// Railway is the price list used for the estimate: CPU $20 per vCPU-month,
// RAM $10 per GB-month, egress $0.05 per GB, volumes $0.15 per GB-month,
// billed per minute (730 hours a month).
var Railway = Prices{
	Date:          "2026-10-22",
	SourceURL:     "https://railway.com/pricing",
	VCPUMonth:     20,
	MemGBMonth:    10,
	EgressGB:      0.05,
	VolumeGBMonth: 0.15,
	HoursPerMonth: 730,
}

// Service is one service instance of the project.
type Service struct {
	EnvID     string
	ServiceID string
	Name      string
}

// Measured is the resource use of one service instance.
type Measured struct {
	VCPUHours      float64
	MemGBHours     float64
	EgressGB       float64
	VolumeGBMonths float64
	BackupBytes    int64
}

// Source measures one service instance over a window.
type Source interface {
	Measure(ctx context.Context, s Service, fromMs, untilMs int64) (Measured, error)
}

// Compute builds the usage response. Rows of one service in several
// environments are summed under the service.
func Compute(ctx context.Context, src Source, services []Service, fromMs, untilMs int64, p Prices) (api.UsageResponse, error) {
	resp := api.UsageResponse{From: fromMs, Until: untilMs, Services: []api.UsageRow{}, Total: api.UsageRow{Name: "Total"}}
	byService := map[string]*api.UsageRow{}
	var order []string
	for _, s := range services {
		m, err := src.Measure(ctx, s, fromMs, untilMs)
		if err != nil {
			return resp, err
		}
		r := byService[s.ServiceID]
		if r == nil {
			r = &api.UsageRow{ServiceID: s.ServiceID, Name: s.Name}
			byService[s.ServiceID] = r
			order = append(order, s.ServiceID)
		}
		r.VCPUHours += m.VCPUHours
		r.MemGBHours += m.MemGBHours
		r.EgressGB += m.EgressGB
		r.VolumeGBMonths += m.VolumeGBMonths
		r.BackupBytes += m.BackupBytes
	}
	sort.SliceStable(order, func(i, j int) bool { return byService[order[i]].Name < byService[order[j]].Name })
	for _, id := range order {
		r := round(*byService[id])
		resp.Services = append(resp.Services, r)
		resp.Total.VCPUHours += r.VCPUHours
		resp.Total.MemGBHours += r.MemGBHours
		resp.Total.EgressGB += r.EgressGB
		resp.Total.VolumeGBMonths += r.VolumeGBMonths
		resp.Total.BackupBytes += r.BackupBytes
	}
	resp.Total = round(resp.Total)
	resp.Railway = api.RailwayEstimate{USD: Estimate(resp.Total, p), PricesDate: p.Date, SourceURL: p.SourceURL}
	return resp, nil
}

// Estimate prices a usage row at p, rounded to cents.
func Estimate(r api.UsageRow, p Prices) float64 {
	h := p.HoursPerMonth
	if h <= 0 {
		h = 730
	}
	usd := r.VCPUHours/h*p.VCPUMonth + r.MemGBHours/h*p.MemGBMonth + r.EgressGB*p.EgressGB + r.VolumeGBMonths*p.VolumeGBMonth
	if p.BucketGBMonth > 0 && !p.BucketIncluded {
		usd += float64(r.BackupBytes) / float64(1<<30) * p.BucketGBMonth
	}
	return math.Round(usd*100) / 100
}

func round(r api.UsageRow) api.UsageRow {
	r.VCPUHours = round3(r.VCPUHours)
	r.MemGBHours = round3(r.MemGBHours)
	r.EgressGB = round3(r.EgressGB)
	r.VolumeGBMonths = round3(r.VolumeGBMonths)
	return r
}

func round3(v float64) float64 { return math.Round(v*1000) / 1000 }
