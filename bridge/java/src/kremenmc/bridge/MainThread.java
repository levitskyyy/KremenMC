package kremenmc.bridge;

import java.util.concurrent.Callable;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.LinkedBlockingQueue;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.TimeoutException;
import java.util.logging.Level;
import java.util.logging.Logger;

/** The Bukkit "server thread": sync events, commands and sync tasks run here. */
final class MainThread {
    private static final Logger LOG = Logger.getLogger("kremenmc-bridge");
    private final LinkedBlockingQueue<Runnable> queue = new LinkedBlockingQueue<>();
    private final Thread thread;
    private volatile boolean running = true;

    MainThread() {
        thread = new Thread(this::loop, "Server thread");
        thread.setDaemon(true);
        thread.start();
    }

    boolean isCurrent() { return Thread.currentThread() == thread; }

    void execute(Runnable r) { queue.add(r); }

    /** Runs c on the main thread and waits; runs inline when already on it. */
    <T> T call(Callable<T> c, long timeoutMs) throws Exception {
        if (isCurrent()) return c.call();
        CompletableFuture<T> f = new CompletableFuture<>();
        queue.add(() -> {
            try {
                f.complete(c.call());
            } catch (Throwable t) {
                f.completeExceptionally(t);
            }
        });
        try {
            return f.get(timeoutMs, TimeUnit.MILLISECONDS);
        } catch (ExecutionException e) {
            if (e.getCause() instanceof Exception ex) throw ex;
            throw e;
        } catch (TimeoutException e) {
            throw new TimeoutException("main thread busy for " + timeoutMs + "ms");
        }
    }

    void stop() {
        running = false;
        queue.add(() -> {});
    }

    private void loop() {
        while (running) {
            Runnable r;
            try {
                r = queue.take();
            } catch (InterruptedException e) {
                return;
            }
            try {
                r.run();
            } catch (Throwable t) {
                LOG.log(Level.SEVERE, "uncaught exception on server thread", t);
            }
        }
    }
}
