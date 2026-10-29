package kremenmc.bridge;

import io.papermc.paper.threadedregions.scheduler.AsyncScheduler;
import io.papermc.paper.threadedregions.scheduler.EntityScheduler;
import io.papermc.paper.threadedregions.scheduler.GlobalRegionScheduler;
import io.papermc.paper.threadedregions.scheduler.RegionScheduler;
import io.papermc.paper.threadedregions.scheduler.ScheduledTask;
import java.util.ArrayList;
import java.util.Comparator;
import java.util.List;
import java.util.Map;
import java.util.PriorityQueue;
import java.util.UUID;
import java.util.concurrent.Callable;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.ConcurrentLinkedQueue;
import java.util.concurrent.Executor;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;
import java.util.concurrent.FutureTask;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.ScheduledFuture;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.atomic.AtomicLong;
import java.util.concurrent.atomic.AtomicReference;
import java.util.function.Consumer;
import java.util.logging.Level;
import java.util.logging.Logger;
import org.bukkit.Location;
import org.bukkit.World;
import org.bukkit.entity.Entity;
import org.bukkit.entity.Player;
import org.bukkit.plugin.IllegalPluginAccessException;
import org.bukkit.plugin.Plugin;
import org.bukkit.scheduler.BukkitRunnable;
import org.bukkit.scheduler.BukkitScheduler;
import org.bukkit.scheduler.BukkitTask;
import org.bukkit.scheduler.BukkitWorker;

/**
 * BukkitScheduler plus the Folia schedulers, all driven by the tick signal
 * from Go. Sync and region/entity tasks run on the main thread; async tasks
 * are released on tick boundaries (Bukkit) or by wall clock (Folia AsyncScheduler)
 * onto a worker pool.
 */
final class Scheduler implements BukkitScheduler {
    private static final Logger LOG = Logger.getLogger("kremenmc-bridge");

    private final MainThread main;
    private final ExecutorService async = Executors.newThreadPerTaskExecutor(Thread.ofPlatform().name("Craft Scheduler Thread - ", 0).daemon().factory());
    private final ScheduledExecutorService timer = Executors.newSingleThreadScheduledExecutor(Thread.ofPlatform().name("kremenmc-async-timer").daemon().factory());
    private final AtomicInteger ids = new AtomicInteger();
    private final AtomicLong currentTick = new AtomicLong();
    private final AtomicLong latestTick = new AtomicLong();
    private final AtomicReference<Boolean> tickQueued = new AtomicReference<>(false);
    private final ConcurrentLinkedQueue<Task> incoming = new ConcurrentLinkedQueue<>();
    private final PriorityQueue<Task> queue = new PriorityQueue<>(Comparator.comparingLong((Task t) -> t.nextRun).thenComparingInt(t -> t.id));
    private final Map<Integer, Task> tasks = new ConcurrentHashMap<>();

    final GlobalRegionScheduler global = new Global();
    final RegionScheduler region = new Region();
    final AsyncScheduler asyncScheduler = new Async();

    Scheduler(MainThread main) {
        this.main = main;
    }

    long currentTick() { return currentTick.get(); }

    /** Called from the IPC thread for every tick notification; coalesces when the main thread lags. */
    void onTick(long tick) {
        latestTick.set(tick);
        if (tickQueued.compareAndSet(false, true)) {
            main.execute(() -> {
                tickQueued.set(false);
                runTick(latestTick.get());
            });
        }
    }

    private void runTick(long tick) {
        currentTick.set(Math.max(tick, currentTick.get() + 1));
        long now = currentTick.get();
        for (Task t; (t = incoming.poll()) != null; ) {
            if (t.nextRun < 0) t.nextRun = now + t.delay;
            queue.add(t);
        }
        List<Task> again = new ArrayList<>();
        while (!queue.isEmpty() && queue.peek().nextRun <= now) {
            Task t = queue.poll();
            if (t.cancelled) {
                tasks.remove(t.id);
                continue;
            }
            if (t.sync) {
                t.execute();
            } else {
                async.execute(t::execute);
            }
            if (t.period > 0 && !t.cancelled) {
                t.nextRun = now + t.period;
                again.add(t);
            } else if (t.sync) {
                tasks.remove(t.id);
            }
        }
        queue.addAll(again);
    }

    void shutdown() {
        for (Task t : tasks.values()) t.cancelled = true;
        tasks.clear();
        timer.shutdownNow();
        async.shutdown();
    }

    final class Task implements BukkitTask {
        final int id;
        final Plugin plugin;
        final Object body;
        final boolean sync;
        final long delay;
        final long period;
        final boolean folia;
        final UUID entity;
        final Runnable retired;
        volatile long nextRun = -1;
        volatile boolean cancelled;
        volatile boolean running;
        volatile boolean ran;
        volatile ScheduledFuture<?> timerFuture;

        Task(Plugin plugin, Object body, boolean sync, long delay, long period, boolean folia, UUID entity, Runnable retired) {
            if (plugin == null) throw new IllegalArgumentException("Plugin cannot be null");
            if (body == null) throw new IllegalArgumentException("Task cannot be null");
            if (!plugin.isEnabled()) throw new IllegalPluginAccessException("Plugin attempted to register task while disabled");
            this.id = ids.incrementAndGet();
            this.plugin = plugin;
            this.body = body;
            this.sync = sync;
            this.delay = Math.max(0, delay);
            this.period = period;
            this.folia = folia;
            this.entity = entity;
            this.retired = retired;
        }

        @SuppressWarnings("unchecked")
        void execute() {
            if (cancelled) return;
            if (entity != null && Bridge.get().players.cached(entity) == null) {
                cancelled = true;
                if (retired != null) retired.run();
                return;
            }
            running = true;
            try {
                if (body instanceof Runnable r) r.run();
                else ((Consumer<Object>) body).accept(this);
            } catch (Throwable t) {
                LOG.log(Level.WARNING, "Plugin " + plugin.getName() + " generated an exception while executing task " + id, t);
            } finally {
                running = false;
                ran = true;
                if (period <= 0) tasks.remove(id);
            }
        }

        @Override public int getTaskId() { return id; }
        @Override public Plugin getOwner() { return plugin; }
        @Override public boolean isSync() { return sync; }
        @Override public boolean isCancelled() { return cancelled; }

        @Override
        public void cancel() {
            cancelTask();
        }

        ScheduledTask.CancelledState cancelTask() {
            if (cancelled) return ScheduledTask.CancelledState.CANCELLED_ALREADY;
            cancelled = true;
            tasks.remove(id);
            ScheduledFuture<?> f = timerFuture;
            if (f != null) f.cancel(false);
            if (running) return period > 0 ? ScheduledTask.CancelledState.NEXT_RUNS_CANCELLED : ScheduledTask.CancelledState.RUNNING;
            if (ran && period <= 0) return ScheduledTask.CancelledState.ALREADY_EXECUTED;
            return ScheduledTask.CancelledState.CANCELLED_BY_CALLER;
        }

        ScheduledTask.ExecutionState getExecutionState() {
            if (running) return cancelled ? ScheduledTask.ExecutionState.CANCELLED_RUNNING : ScheduledTask.ExecutionState.RUNNING;
            if (cancelled) return ScheduledTask.ExecutionState.CANCELLED;
            if (ran && period <= 0) return ScheduledTask.ExecutionState.FINISHED;
            return ScheduledTask.ExecutionState.IDLE;
        }
    }

    // ScheduledTask.cancel() and BukkitTask.cancel() clash on return type, so Folia callers get a wrapper.
    private final class FoliaTask implements ScheduledTask {
        final Task t;
        FoliaTask(Task t) { this.t = t; }
        @Override public Plugin getOwningPlugin() { return t.plugin; }
        @Override public boolean isRepeatingTask() { return t.period > 0; }
        @Override public ScheduledTask.CancelledState cancel() { return t.cancelTask(); }
        @Override public ScheduledTask.ExecutionState getExecutionState() { return t.getExecutionState(); }
    }

    private Task schedule(Plugin p, Object body, boolean sync, long delay, long period) {
        Task t = new Task(p, body, sync, delay, period, false, null, null);
        tasks.put(t.id, t);
        incoming.add(t);
        return t;
    }

    private ScheduledTask folia(Plugin p, Consumer<ScheduledTask> body, long delay, long period, UUID entity, Runnable retired) {
        Task[] holder = new Task[1];
        Consumer<Object> wrapped = o -> body.accept(new FoliaTask(holder[0]));
        Task t = new Task(p, wrapped, true, Math.max(1, delay), period, true, entity, retired);
        holder[0] = t;
        tasks.put(t.id, t);
        incoming.add(t);
        return new FoliaTask(t);
    }

    private static Runnable runnable(Consumer<? super BukkitTask> c, BukkitTask[] self) {
        return () -> c.accept(self[0]);
    }

    private BukkitTask consumerTask(Plugin p, Consumer<? super BukkitTask> c, boolean sync, long delay, long period) {
        BukkitTask[] self = new BukkitTask[1];
        Task t = new Task(p, runnable(c, self), sync, delay, period, false, null, null);
        self[0] = t;
        tasks.put(t.id, t);
        incoming.add(t);
        return t;
    }

    // BukkitScheduler

    @Override public int scheduleSyncDelayedTask(Plugin p, Runnable r, long delay) { return schedule(p, r, true, delay, -1).id; }
    @Override public int scheduleSyncDelayedTask(Plugin p, BukkitRunnable r, long delay) { return r.runTaskLater(p, delay).getTaskId(); }
    @Override public int scheduleSyncDelayedTask(Plugin p, Runnable r) { return scheduleSyncDelayedTask(p, r, 0); }
    @Override public int scheduleSyncDelayedTask(Plugin p, BukkitRunnable r) { return r.runTask(p).getTaskId(); }
    @Override public int scheduleSyncRepeatingTask(Plugin p, Runnable r, long delay, long period) { return schedule(p, r, true, delay, Math.max(1, period)).id; }
    @Override public int scheduleSyncRepeatingTask(Plugin p, BukkitRunnable r, long delay, long period) { return r.runTaskTimer(p, delay, period).getTaskId(); }
    @Override public int scheduleAsyncDelayedTask(Plugin p, Runnable r, long delay) { return schedule(p, r, false, delay, -1).id; }
    @Override public int scheduleAsyncDelayedTask(Plugin p, Runnable r) { return scheduleAsyncDelayedTask(p, r, 0); }
    @Override public int scheduleAsyncRepeatingTask(Plugin p, Runnable r, long delay, long period) { return schedule(p, r, false, delay, Math.max(1, period)).id; }

    @Override
    public <T> Future<T> callSyncMethod(Plugin p, Callable<T> c) {
        FutureTask<T> f = new FutureTask<>(c);
        schedule(p, f, true, 0, -1);
        return f;
    }

    @Override
    public void cancelTask(int id) {
        Task t = tasks.get(id);
        if (t != null) t.cancelTask();
    }

    @Override
    public void cancelTasks(Plugin p) {
        for (Task t : tasks.values()) if (t.plugin == p) t.cancelTask();
    }

    @Override public boolean isCurrentlyRunning(int id) { Task t = tasks.get(id); return t != null && t.running; }
    @Override public boolean isQueued(int id) { Task t = tasks.get(id); return t != null && !t.cancelled; }
    @Override public List<BukkitWorker> getActiveWorkers() { return new ArrayList<>(); }

    @Override
    public List<BukkitTask> getPendingTasks() {
        List<BukkitTask> out = new ArrayList<>();
        for (Task t : tasks.values()) if (!t.cancelled && !t.folia) out.add(t);
        return out;
    }

    @Override public BukkitTask runTask(Plugin p, Runnable r) { return schedule(p, r, true, 0, -1); }
    @Override public void runTask(Plugin p, Consumer<? super BukkitTask> c) { consumerTask(p, c, true, 0, -1); }
    @Override public BukkitTask runTask(Plugin p, BukkitRunnable r) { return r.runTask(p); }
    @Override public BukkitTask runTaskAsynchronously(Plugin p, Runnable r) { return schedule(p, r, false, 0, -1); }
    @Override public void runTaskAsynchronously(Plugin p, Consumer<? super BukkitTask> c) { consumerTask(p, c, false, 0, -1); }
    @Override public BukkitTask runTaskAsynchronously(Plugin p, BukkitRunnable r) { return r.runTaskAsynchronously(p); }
    @Override public BukkitTask runTaskLater(Plugin p, Runnable r, long d) { return schedule(p, r, true, d, -1); }
    @Override public void runTaskLater(Plugin p, Consumer<? super BukkitTask> c, long d) { consumerTask(p, c, true, d, -1); }
    @Override public BukkitTask runTaskLater(Plugin p, BukkitRunnable r, long d) { return r.runTaskLater(p, d); }
    @Override public BukkitTask runTaskLaterAsynchronously(Plugin p, Runnable r, long d) { return schedule(p, r, false, d, -1); }
    @Override public void runTaskLaterAsynchronously(Plugin p, Consumer<? super BukkitTask> c, long d) { consumerTask(p, c, false, d, -1); }
    @Override public BukkitTask runTaskLaterAsynchronously(Plugin p, BukkitRunnable r, long d) { return r.runTaskLaterAsynchronously(p, d); }
    @Override public BukkitTask runTaskTimer(Plugin p, Runnable r, long d, long per) { return schedule(p, r, true, d, Math.max(1, per)); }
    @Override public void runTaskTimer(Plugin p, Consumer<? super BukkitTask> c, long d, long per) { consumerTask(p, c, true, d, Math.max(1, per)); }
    @Override public BukkitTask runTaskTimer(Plugin p, BukkitRunnable r, long d, long per) { return r.runTaskTimer(p, d, per); }
    @Override public BukkitTask runTaskTimerAsynchronously(Plugin p, Runnable r, long d, long per) { return schedule(p, r, false, d, Math.max(1, per)); }
    @Override public void runTaskTimerAsynchronously(Plugin p, Consumer<? super BukkitTask> c, long d, long per) { consumerTask(p, c, false, d, Math.max(1, per)); }
    @Override public BukkitTask runTaskTimerAsynchronously(Plugin p, BukkitRunnable r, long d, long per) { return r.runTaskTimerAsynchronously(p, d, per); }

    @Override
    public Executor getMainThreadExecutor(Plugin p) {
        return r -> runTask(p, r);
    }

    // Folia. Regions are not split on the Java side: region and global tasks
    // share the main thread, so "owned by current region" equals "on the main thread".

    private final class Global implements GlobalRegionScheduler {
        @Override public void execute(Plugin p, Runnable r) { runTask(p, r); }
        @Override public ScheduledTask run(Plugin p, Consumer<ScheduledTask> c) { return folia(p, c, 1, -1, null, null); }
        @Override public ScheduledTask runDelayed(Plugin p, Consumer<ScheduledTask> c, long d) { return folia(p, c, d, -1, null, null); }
        @Override public ScheduledTask runAtFixedRate(Plugin p, Consumer<ScheduledTask> c, long d, long per) { return folia(p, c, d, Math.max(1, per), null, null); }
        @Override public void cancelTasks(Plugin p) { for (Task t : tasks.values()) if (t.plugin == p && t.folia && t.entity == null) t.cancelTask(); }
    }

    private final class Region implements RegionScheduler {
        @Override public void execute(Plugin p, World w, int cx, int cz, Runnable r) { runTask(p, r); }
        @Override public ScheduledTask run(Plugin p, World w, int cx, int cz, Consumer<ScheduledTask> c) { return folia(p, c, 1, -1, null, null); }
        @Override public ScheduledTask runDelayed(Plugin p, World w, int cx, int cz, Consumer<ScheduledTask> c, long d) { return folia(p, c, d, -1, null, null); }
        @Override public ScheduledTask runAtFixedRate(Plugin p, World w, int cx, int cz, Consumer<ScheduledTask> c, long d, long per) { return folia(p, c, d, Math.max(1, per), null, null); }
        @Override public void execute(Plugin p, Location l, Runnable r) { runTask(p, r); }
    }

    final class EntitySched implements EntityScheduler {
        private final UUID entity;
        EntitySched(UUID entity) { this.entity = entity; }

        @Override
        public boolean execute(Plugin p, Runnable r, Runnable retired, long delay) {
            if (Bridge.get().players.cached(entity) == null) return false;
            folia(p, t -> r.run(), delay, -1, entity, retired);
            return true;
        }

        @Override public ScheduledTask run(Plugin p, Consumer<ScheduledTask> c, Runnable retired) { return entityTask(p, c, retired, 1, -1); }
        @Override public ScheduledTask runDelayed(Plugin p, Consumer<ScheduledTask> c, Runnable retired, long d) { return entityTask(p, c, retired, d, -1); }
        @Override public ScheduledTask runAtFixedRate(Plugin p, Consumer<ScheduledTask> c, Runnable retired, long d, long per) { return entityTask(p, c, retired, d, Math.max(1, per)); }

        private ScheduledTask entityTask(Plugin p, Consumer<ScheduledTask> c, Runnable retired, long d, long per) {
            if (Bridge.get().players.cached(entity) == null) return null;
            return folia(p, c, d, per, entity, retired);
        }
    }

    EntityScheduler entityScheduler(Entity e) {
        return new EntitySched(e instanceof Player pl ? pl.getUniqueId() : e.getUniqueId());
    }

    private final class Async implements AsyncScheduler {
        @Override
        public ScheduledTask runNow(Plugin p, Consumer<ScheduledTask> c) {
            return runDelayed(p, c, 0, TimeUnit.MILLISECONDS);
        }

        @Override
        public ScheduledTask runDelayed(Plugin p, Consumer<ScheduledTask> c, long delay, TimeUnit unit) {
            Task[] holder = new Task[1];
            Task t = new Task(p, (Consumer<Object>) o -> c.accept(new FoliaTask(holder[0])), false, 0, -1, true, null, null);
            holder[0] = t;
            tasks.put(t.id, t);
            t.timerFuture = timer.schedule(() -> async.execute(t::execute), delay, unit);
            return new FoliaTask(t);
        }

        @Override
        public ScheduledTask runAtFixedRate(Plugin p, Consumer<ScheduledTask> c, long delay, long period, TimeUnit unit) {
            if (period <= 0) throw new IllegalArgumentException("period must be positive");
            Task[] holder = new Task[1];
            Task t = new Task(p, (Consumer<Object>) o -> c.accept(new FoliaTask(holder[0])), false, 0, 1, true, null, null);
            holder[0] = t;
            tasks.put(t.id, t);
            t.timerFuture = timer.scheduleAtFixedRate(() -> async.execute(t::execute), delay, period, unit);
            return new FoliaTask(t);
        }

        @Override
        public void cancelTasks(Plugin p) {
            for (Task t : tasks.values()) if (t.plugin == p && t.timerFuture != null) t.cancelTask();
        }
    }
}
