# kube-life

Toy TUI that simulates **Conway's Game of Life** together with a **Deployment-style reconcile loop** (ReplicaSets by **revision**) on a **virtual in-process cluster**. It does **not** connect to Kubernetes (no kubeconfig, context, or namespace).

This is not a faithful Kubernetes simulator. It is a visual metaphor: Game of Life creates a naturally changing world, and a Deployment-like controller periodically nudges that world back toward declared replica counts.

## Demo

[kube-life demo](docs/assets/kube-life.mp4)

## Quick start

Requires **Go 1.22+**.

```bash
go build -o kube-life ./cmd/kube-life
./kube-life -nodes 4 -w 40 -h 15 -seed 1 -tick-ms 300
```

Recommended terminal width: **80+ columns**. The default `-w 40` grid is designed to fit an 80-column terminal.

Once the TUI is running, type commands after the `>` prompt:

```text
scale --replicas=30
rollout restart
drain nodes 1
taint nodes 2 key=value:NoExecute
```

`scale --replicas=<n>` is the easiest way to turn on the Deployment controller. Until then, only Game of Life changes the grid.

## How to read the TUI

- Grid cells:
  - `#`: existing alive cell
  - `*`: born by Game of Life on this tick
  - `@`: spawned by Deployment reconcile on this tick
  - `^`: relocated by drain on this tick
  - `.`: dead cell
- Cell colors show **ReplicaSet revision**.
- `|` and `-` mark node partition boundaries.
- Gray dead cells are in tainted, cordoned, or draining node regions.
- `ctrl=idle|observe|apply` shows the last Deployment controller phase.
- `Status:` shows ongoing progress, currently drain progress.
- `Event:` shows the latest important internal event, such as `drain done`, `drain blocked`, `scale-up throttled`, `rollout done`, or `eviction ...`.

## Concept

- **Natural change**: one Game of Life step per tick (the TUI shows **GoL ticks** = how many steps have run while not paused). Alive cells carry a **ReplicaSet revision** id; births take the majority neighbor RS revision (tie: smallest), or the active Deployment RS revision as fallback.
- **Declarative control**: when Deployment is enabled, the controller runs on a **3-tick cadence** while Game of Life still runs **every** tick: **idle** (no reconcile mutations) → **observe** (internal log only) → **apply** (rollout step + scale toward `desired`). That separates “natural” drift from controller action so behavior is easier to follow.
- **Rollout**: `rollout restart` bumps **generation**, creates a new ReplicaSet revision (same image), and performs a **stepwise** rollout on each **apply** tick. Each step shifts 10% of the effective desired count, rounded up with a minimum of 1, from the old RS to the new RS until the old RS is empty and removed. The TUI labels these as `oldRSRev`, `newRSRev`, and `activeRSRev`.
- **Node constraints**: taints (`NoSchedule`, `NoExecute`) affect reconcile spawns; `NoExecute` also evicts every tick on that node region.

Each tick: **GoL step → NoExecute eviction (if any) → deployment phase (if on: idle | observe | apply) → render**. The status line shows `ctrl=idle|observe|apply` for the last controller step.

## Cell change rules

Cells are both Game of Life cells and workload units in this simulation. That means they may change naturally through GoL, and they may also be corrected by the Deployment controller.

On each tick, kube-life applies changes in this order:

1. **Game of Life** runs on nodes that are not cordoned or draining.
   - Alive cells survive with 2 or 3 alive neighbors.
   - Dead cells become alive with exactly 3 alive neighbors.
   - All other cells become dead.
   - GoL births inherit the majority neighbor RS revision; ties use the smallest revision. If no neighbor has an RS revision, the active Deployment RS revision is used as fallback.
2. **NoExecute eviction** clears alive cells on nodes with a `NoExecute` taint.
3. **Drain relocation** moves at most one alive cell per tick from the draining node to a schedulable dead cell on another node.
   - In this simulation, alive cells are treated as generic "workload units" that can always be relocated. Real Kubernetes drain behavior depends on pod types (e.g. DaemonSets), controllers, local data, disruption budgets, etc.
4. **Deployment controller**, if enabled, runs one phase:
   - `idle`: no cell changes.
   - `observe`: records internal state only; no cell changes.
   - `apply`: rolls out and scales ReplicaSets toward desired counts. Scale-up turns schedulable dead cells alive with the ReplicaSet revision; scale-down turns alive cells of that RS revision dead.

Deployment reconcile does not replace Game of Life. It corrects the world after natural GoL changes have already happened.

## TUI behavior details

- **Birth markers (1 tick only)**:
  - `*`: born by **GoL**
  - `@`: spawned by **Deployment** (reconcile scale-up)
  - `^`: relocated by **drain**
  - otherwise Alive is `#` (colored by ReplicaSet revision)
- **Tainted / cordoned / draining regions**:
  - For nodes with **`NoSchedule` or `NoExecute` taint**, or nodes that are **cordoned/draining**, dead cells (`.`) are rendered **gray**.
  - While a node is **draining or cordoned**, **GoL is not applied** to that node’s region (cells are frozen).
- **Drain**:
  - `drain nodes <id>` starts draining and moves **1 pod per tick** to make it observable.
  - The node becomes **cordoned** and stays unschedulable until `uncordon nodes <id>`.
- **Status / Event lines**:
  - `Status:` shows **ongoing progress**. Currently it is used only for drain progress:
    - `(none)` when not draining
    - `drain: node=<id> remaining=<n> moved=<n> blocked=<n>` while draining
  - `Event:` shows the **latest important internal event** (e.g. `drain done`, `drain blocked`, `scale-up throttled`, `rollout done`, `eviction ...`).

## Build

Requires **Go 1.22+**.

```bash
go build -o kube-life ./cmd/kube-life
```

## Run

```bash
./kube-life -nodes 1 -w 40 -h 15 -seed 1 -tick-ms 500
```

Flags:

| Flag | Default | Meaning |
|------|---------|---------|
| `-nodes` | `1` | Node count (max **16**; grid is partitioned into rectangular tiles, `cols=ceil(sqrt(n))`). Values above max or `w*h` are clamped with a log line. |
| `-w`, `-h` | `40`, `15` | Grid size |
| `-seed` | `1` | Seed for the **initial random** layout (Bernoulli `p=0.18` per cell) |
| `-tick-ms` | `500` | Simulation tick interval |

The workload line shows **Deployment image** (starts at `kube-life/cell:latest`) and display-only CPU; `maxPods` follows grid size.

## Commands (stdin line after `>`)

### Cluster-like commands (kubectl-style)

| Command | Effect |
|---------|--------|
| `scale --replicas=<n>` | Enable Deployment; set `replicas` (clamped to `maxPods`). Creates ReplicaSet **RSRev=1** on first use. |
| `rollout status` | Print rollout / ReplicaSet revision summary in the sticky command feedback area. |
| `rollout restart` | Like `kubectl rollout restart`: bumps `generation`, starts rollout **without changing image**. |
| `drain nodes <id>` | Evict pods from node and try to place them on other nodes (respects taints / scheduler constraints). |
| `uncordon nodes <id>` | Allow scheduling onto a previously drained node again. |
| `taint nodes <id> key=value:NoSchedule` | Apply `NoSchedule` taint (key/value are ignored; effect matters). |
| `taint nodes <id> key=value:NoExecute` | Apply `NoExecute` taint (immediate clear + per-tick eviction). |
| `untaint nodes <id> <key>` | Clear taint (key is ignored). |

### Simulation commands

| Command | Effect |
|---------|--------|
| `sim pause` / `sim resume` | Pause or resume simulation ticks. |
| `sim gol pause` / `sim gol resume` | Pause/resume **Game of Life** steps while keeping deployment/ops ticks running. |
| `sim speed <ms>` / `sim speed default` | Change tick interval (`10..60000`) or reset to default (500ms). |
| `sim reset` | Reset the whole simulation to the initial state (same `-w/-h/-nodes/-seed`). |
| `sim deploy enable` / `sim deploy disable` | Enable/disable the Deployment controller without resetting the GoL grid. `disable` keeps desired; `enable` sets `replicas` from the current alive count. |

Notes: `kubectl` prefix is accepted but intentionally omitted here.

**Keys**: `q` or `Ctrl+C` quit. **Feedback**: shows the last entered command and its output (sticky). **Input**: max 256 characters.

## Notes

- GoL uses a **fixed dead boundary** (no torus).
- While Deployment is off, **only GoL** changes the grid. With Deployment on, reconcile may add/remove live cells **per ReplicaSet revision**.
- Natural GoL can temporarily push live count **above** `maxPods`; reconcile corrects toward per-RS `desired` within `maxPods`.
- Grid view inserts `|` / `-` where the owning node changes (partition boundary).
- Taints: `NoSchedule` blocks new reconcile spawns on that node; `NoExecute` blocks spawns and clears that node region every tick.
