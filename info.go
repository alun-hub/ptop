package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"ptop/internal/bench"
)

func runInfo(args []string) int {
	for _, a := range args {
		if a == "-h" || a == "--help" {
			fmt.Println("usage: ptop info    show a snapshot of this machine (CPU, memory, disks, NICs, cloud)")
			return 0
		}
		fmt.Fprintf(os.Stderr, "unknown flag: %s\n", a)
		return 2
	}
	inv := bench.Inventorize()
	fmt.Print(formatInventory(inv))
	fmt.Print(formatAdvice(bench.Advise(inv)))
	return 0
}

func formatAdvice(adv []bench.Advice) string {
	var b strings.Builder
	b.WriteString("\n== Recommendations ==\n")
	if len(adv) == 0 {
		b.WriteString("  Nothing stands out - system configuration looks fine for benchmarking.\n")
		return b.String()
	}
	for _, a := range adv {
		fmt.Fprintf(&b, "  [%s] %s\n", a.Severity.Label(), a.Title)
		fmt.Fprintf(&b, "         %s\n", a.Detail)
		if a.Fix != "" {
			fmt.Fprintf(&b, "         $ %s\n", a.Fix)
		}
	}
	return b.String()
}

func formatInventory(inv bench.Inventory) string {
	var b strings.Builder
	row := func(k, v string) {
		if v == "" {
			v = "-"
		}
		fmt.Fprintf(&b, "  %-16s %s\n", k, v)
	}

	b.WriteString("== System ==\n")
	row("Hostname", inv.Hostname)
	row("OS", inv.OSName)
	row("Kernel", strings.TrimSpace(inv.Kernel+" "+inv.Arch))
	if inv.Uptime > 0 {
		row("Uptime", humanUptime(inv.Uptime))
	}
	env := inv.Virt
	if inv.CloudVendor != "" {
		env = inv.CloudVendor + " (" + inv.Virt + ")"
	}
	row("Environment", env)
	if inv.ProductName != "" {
		row("Product", inv.ProductName)
	}

	b.WriteString("\n== CPU ==\n")
	row("Model", inv.CPUModel)
	cores := fmt.Sprintf("%d logical", inv.LogicalCPUs)
	if inv.PhysicalCores > 0 && inv.PhysicalCores != inv.LogicalCPUs {
		cores = fmt.Sprintf("%d physical / %d logical", inv.PhysicalCores, inv.LogicalCPUs)
	}
	if inv.Sockets > 1 {
		cores += fmt.Sprintf("  (%d sockets)", inv.Sockets)
	}
	row("Cores", cores)
	if inv.CPUMHzMax > 0 {
		row("Max freq", fmt.Sprintf("%.0f MHz", inv.CPUMHzMax))
	}
	row("Governor", inv.Governor)
	if len(inv.CPUFlags) > 0 {
		row("Features", strings.Join(inv.CPUFlags, " "))
	}

	b.WriteString("\n== Memory ==\n")
	row("RAM", fmt.Sprintf("%.1f GB total, %.1f GB available", inv.MemTotalGB, inv.MemAvailGB))
	if inv.SwapTotalGB > 0 {
		row("Swap", fmt.Sprintf("%.1f GB", inv.SwapTotalGB))
	} else {
		row("Swap", "none")
	}
	if inv.Swappiness >= 0 {
		row("Swappiness", fmt.Sprintf("%d", inv.Swappiness))
	}
	row("Transparent HP", inv.THP)
	if inv.NUMANodes > 1 {
		row("NUMA nodes", fmt.Sprintf("%d", inv.NUMANodes))
	}

	if len(inv.Disks) > 0 {
		b.WriteString("\n== Disks ==\n")
		fmt.Fprintf(&b, "  %-12s %-24s %10s  %-8s %s\n", "device", "model", "size", "type", "scheduler")
		for _, d := range inv.Disks {
			typ := "SSD"
			if d.Rotational {
				typ = "HDD"
			}
			fmt.Fprintf(&b, "  %-12s %-24s %8.0f GB  %-8s %s\n",
				d.Device, cut(orDash(d.Model), 24), d.SizeGB, typ, orDash(d.Scheduler))
		}
	}

	if len(inv.NICs) > 0 {
		b.WriteString("\n== Network ==\n")
		for _, n := range inv.NICs {
			speed := "unknown speed"
			if n.SpeedMbps > 0 {
				speed = fmt.Sprintf("%d Mbps", n.SpeedMbps)
			}
			fmt.Fprintf(&b, "  %-12s %-6s %s\n", n.Name, orDash(n.State), speed)
		}
	}

	return b.String()
}

func humanUptime(d time.Duration) string {
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60
	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh %dm", hours, mins)
	default:
		return fmt.Sprintf("%dm", mins)
	}
}
