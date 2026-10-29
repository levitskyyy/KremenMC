package kremenmc.bridge;

import java.io.File;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.logging.Level;
import java.util.logging.Logger;
import net.kyori.adventure.text.Component;
import net.kyori.adventure.text.format.NamedTextColor;
import org.bukkit.command.CommandSender;
import org.bukkit.plugin.Plugin;

/** Entry point of the sidecar JVM. Started by internal/bridge with KREMENMC_BRIDGE_ADDR and KREMENMC_BRIDGE_TOKEN. */
public final class Main {
    private static final Logger LOG = Logger.getLogger("kremenmc-bridge");
    private static final long LOAD_TIMEOUT_MS = 10 * 60_000;
    private static final long CALL_TIMEOUT_MS = 30_000;

    private Main() {}

    public static void main(String[] args) throws Exception {
        LogSetup.install();
        String addr = System.getenv("KREMENMC_BRIDGE_ADDR");
        String token = System.getenv("KREMENMC_BRIDGE_TOKEN");
        if (addr == null || token == null) {
            System.err.println("kremenmc bridge must be started by the kremenmc server");
            System.exit(2);
        }
        Conn conn = Conn.connect(addr);
        CountDownLatch ready = new CountDownLatch(1);
        Bridge[] holder = new Bridge[1];
        conn.handlers((op, a) -> {
            if (!ready.await(60, TimeUnit.SECONDS)) throw new IllegalStateException("bridge not initialised");
            return handle(holder[0], op, a);
        }, (op, a) -> {
            if (op == Wire.OP_TICK && holder[0] != null) holder[0].scheduler.onTick(Wire.num(a, 0));
        });
        Thread reader = new Thread(conn::serve, "kremenmc-ipc");
        reader.start();

        try {
            Map<String, Object> info = Wire.map(conn.call(Wire.OP_HELLO, token, Wire.PROTOCOL_VERSION, Runtime.version().toString()).get(0));
            holder[0] = new Bridge(conn, info);
        } catch (Throwable t) {
            LOG.log(Level.SEVERE, "bridge initialisation failed", t);
            System.exit(1);
        }
        ready.countDown();

        reader.join();
        LOG.info("connection to kremenmc closed; stopping");
        shutdown(holder[0]);
        System.exit(0);
    }

    private static Object[] handle(Bridge b, int op, List<Object> a) throws Exception {
        switch (op) {
            case Wire.OP_LOAD_PLUGINS:
                return new Object[] {b.main.call(() -> load(b, Wire.list(a.get(0)), Wire.str(a, 1)), LOAD_TIMEOUT_MS)};
            case Wire.OP_EVENT:
                return Events.fire(b, Wire.str(a, 0), Wire.map(a.get(1)), Wire.bool(a, 2));
            case Wire.OP_COMMAND: {
                CommandSender sender = b.sender(Wire.uuid(a, 0));
                String line = Wire.str(a, 1);
                return new Object[] {b.main.call(() -> command(b, sender, line), CALL_TIMEOUT_MS)};
            }
            case Wire.OP_TAB_COMPLETE: {
                CommandSender sender = b.sender(Wire.uuid(a, 0));
                String line = Wire.str(a, 1);
                List<String> res = b.main.call(() -> b.commandMap.tabComplete(sender, line), CALL_TIMEOUT_MS);
                return new Object[] {res == null ? List.of() : res};
            }
            case Wire.OP_SHUTDOWN: {
                shutdown(b);
                Thread t = new Thread(() -> {
                    try {
                        Thread.sleep(200);
                    } catch (InterruptedException ignored) {
                    }
                    System.exit(0);
                });
                t.setDaemon(true);
                t.start();
                return new Object[0];
            }
            default:
                throw new IllegalArgumentException("unknown op " + op);
        }
    }

    private static List<Object> load(Bridge b, List<Object> jars, String pluginsDir) {
        if (!pluginsDir.isEmpty()) b.pluginsDir = new File(pluginsDir);
        for (Object o : jars) {
            File f = new File(String.valueOf(o));
            try {
                b.pluginManager.loadPlugin(f);
            } catch (Throwable t) {
                LOG.log(Level.SEVERE, "Could not load '" + f.getPath() + "'", t);
            }
        }
        for (Plugin p : b.pluginManager.all()) b.pluginManager.enablePlugin(p);
        b.pluginManager.listenersChanged();
        List<Object> out = new ArrayList<>();
        for (Plugin p : b.pluginManager.all()) {
            Map<String, Object> m = new LinkedHashMap<>();
            m.put("name", p.getName());
            m.put("version", p.getPluginMeta().getVersion());
            m.put("state", p.isEnabled() ? "enabled" : "failed");
            out.add(m);
        }
        return out;
    }

    private static boolean command(Bridge b, CommandSender sender, String line) {
        try {
            return b.commandMap.dispatch(sender, line);
        } catch (Throwable t) {
            LOG.log(Level.SEVERE, "command '" + line + "' failed", t);
            sender.sendMessage(Component.text("An internal error occurred while attempting to perform this command", NamedTextColor.RED));
            return true;
        }
    }

    private static volatile boolean stopped;

    private static void shutdown(Bridge b) {
        if (b == null || stopped) return;
        stopped = true;
        try {
            b.main.call(() -> {
                b.pluginManager.disablePlugins();
                return null;
            }, CALL_TIMEOUT_MS);
        } catch (Exception e) {
            LOG.log(Level.WARNING, "disabling plugins failed", e);
        }
        b.scheduler.shutdown();
        b.main.stop();
    }
}
