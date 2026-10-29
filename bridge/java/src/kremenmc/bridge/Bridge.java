package kremenmc.bridge;

import java.io.File;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.UUID;
import java.util.logging.Logger;
import org.bukkit.Bukkit;
import org.bukkit.Server;
import org.bukkit.command.CommandSender;
import org.bukkit.command.ConsoleCommandSender;
import org.bukkit.command.SimpleCommandMap;
import org.bukkit.plugin.SimpleServicesManager;
import org.bukkit.plugin.messaging.StandardMessenger;

/** Process-wide state of the Java side. */
final class Bridge {
    private static volatile Bridge instance;

    static Bridge get() { return instance; }

    final Logger log = Logger.getLogger("kremenmc-bridge");
    final Conn conn;
    final Map<String, Object> info;
    final MainThread main = new MainThread();
    final Scheduler scheduler = new Scheduler(main);
    final Players players = new Players(this);
    final Worlds worlds = new Worlds(this);
    final PluginManagerImpl pluginManager = new PluginManagerImpl(this);
    final SimpleServicesManager services = new SimpleServicesManager();
    final StandardMessenger messenger = new StandardMessenger();
    final ConsoleCommandSender console;
    final Server server;
    final SimpleCommandMap commandMap;
    volatile File pluginsDir = new File("plugins");

    Bridge(Conn conn, Map<String, Object> info) {
        this.conn = conn;
        this.info = info;
        instance = this;
        this.console = Stubs.proxy(ConsoleCommandSender.class, new Players.ConsoleImpl(this));
        this.server = Stubs.proxy(Server.class, new ServerImpl(this));
        Bukkit.setServer(server);
        this.commandMap = new SimpleCommandMap(server, new HashMap<>());
    }

    String infoString(String k, String def) {
        Object v = info.get(k);
        return v instanceof String s && !s.isEmpty() ? s : def;
    }

    long infoLong(String k, long def) {
        return info.get(k) instanceof Number n ? n.longValue() : def;
    }

    CommandSender sender(UUID id) {
        if (id == null) return console;
        CommandSender p = players.get(id);
        if (p == null) throw new IllegalArgumentException("player " + id + " is not online");
        return p;
    }

    void sendMessage(UUID target, String json) {
        if (json != null) conn.notify(Wire.OP_SEND_MESSAGE, target, json);
    }

    List<Object> call(int op, Object... args) {
        return conn.call(op, args);
    }
}
