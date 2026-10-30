package kremenmc.bridge;

import com.mojang.brigadier.CommandDispatcher;
import com.mojang.brigadier.Message;
import com.mojang.brigadier.ParseResults;
import com.mojang.brigadier.arguments.ArgumentType;
import com.mojang.brigadier.arguments.BoolArgumentType;
import com.mojang.brigadier.arguments.DoubleArgumentType;
import com.mojang.brigadier.arguments.FloatArgumentType;
import com.mojang.brigadier.arguments.IntegerArgumentType;
import com.mojang.brigadier.arguments.LongArgumentType;
import com.mojang.brigadier.arguments.StringArgumentType;
import com.mojang.brigadier.builder.LiteralArgumentBuilder;
import com.mojang.brigadier.builder.RequiredArgumentBuilder;
import com.mojang.brigadier.context.StringRange;
import com.mojang.brigadier.exceptions.CommandSyntaxException;
import com.mojang.brigadier.exceptions.SimpleCommandExceptionType;
import com.mojang.brigadier.suggestion.Suggestion;
import com.mojang.brigadier.suggestion.Suggestions;
import com.mojang.brigadier.tree.ArgumentCommandNode;
import com.mojang.brigadier.tree.CommandNode;
import com.mojang.brigadier.tree.LiteralCommandNode;
import com.mojang.brigadier.tree.RootCommandNode;
import io.papermc.paper.command.brigadier.BasicCommand;
import io.papermc.paper.command.brigadier.CommandRegistrationFlag;
import io.papermc.paper.command.brigadier.CommandSourceStack;
import io.papermc.paper.command.brigadier.Commands;
import io.papermc.paper.command.brigadier.MessageComponentSerializer;
import io.papermc.paper.command.brigadier.argument.BridgeArgumentProvider;
import io.papermc.paper.command.brigadier.argument.CustomArgumentType;
import io.papermc.paper.plugin.configuration.PluginMeta;
import io.papermc.paper.plugin.lifecycle.event.LifecycleEvent;
import io.papermc.paper.plugin.lifecycle.event.LifecycleEventManager;
import io.papermc.paper.plugin.lifecycle.event.handler.LifecycleEventHandler;
import io.papermc.paper.plugin.lifecycle.event.handler.configuration.LifecycleEventHandlerConfiguration;
import io.papermc.paper.plugin.lifecycle.event.registrar.ReloadableRegistrarEvent;
import io.papermc.paper.plugin.lifecycle.event.types.BridgeLifecycleEventTypeProvider;
import java.lang.reflect.Field;
import java.util.ArrayList;
import java.util.Collection;
import java.util.Comparator;
import java.util.LinkedHashMap;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.Set;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.concurrent.TimeUnit;
import java.util.logging.Level;
import net.kyori.adventure.text.Component;
import net.kyori.adventure.text.ComponentLike;
import net.kyori.adventure.text.format.NamedTextColor;
import net.kyori.adventure.text.format.TextDecoration;
import net.kyori.adventure.text.serializer.plain.PlainTextComponentSerializer;
import org.bukkit.Location;
import org.bukkit.World;
import org.bukkit.command.CommandSender;
import org.bukkit.entity.Entity;
import org.bukkit.entity.Player;
import org.bukkit.plugin.Plugin;

/**
 * Paper's brigadier command API: lifecycle handlers for COMMANDS, the
 * registrar, command sources, and the bridge between the Java dispatcher
 * and the Go command tree (execution and suggestions run here).
 */
final class Brigadier {
    private static final SimpleCommandExceptionType NOT_PLAYER = new SimpleCommandExceptionType(new ComponentMessage(Component.translatable("permissions.requires.player")));
    private static final SimpleCommandExceptionType NOT_ENTITY = new SimpleCommandExceptionType(new ComponentMessage(Component.translatable("permissions.requires.entity")));

    private final Bridge b;
    final CommandDispatcher<CommandSourceStack> dispatcher = new CommandDispatcher<>();
    private final List<Handler> handlers = new CopyOnWriteArrayList<>();
    private final Map<String, Registered> commands = new ConcurrentHashMap<>();
    private volatile boolean fired;

    private record Handler(Plugin plugin, String event, int order, boolean monitor, LifecycleEventHandler<LifecycleEvent> handler) {}

    private record Registered(String plugin, LiteralCommandNode<CommandSourceStack> node, String description, List<String> aliases) {}

    Brigadier(Bridge b) { this.b = b; }

    LifecycleEventManager<Plugin> manager(Plugin plugin) {
        return new LifecycleEventManager<>() {
            @Override
            @SuppressWarnings("unchecked")
            public void registerEventHandler(LifecycleEventHandlerConfiguration<? super Plugin> cfg) {
                if (!(cfg instanceof BridgeLifecycleEventTypeProvider.Config c)) throw new IllegalArgumentException("foreign handler configuration " + cfg);
                Handler h = new Handler(plugin, c.eventName(), c.order(), c.isMonitor(), (LifecycleEventHandler<LifecycleEvent>) c.handler());
                handlers.add(h);
                if (fired && h.event.equals("commands")) b.main.execute(() -> run(List.of(h), ReloadableRegistrarEvent.Cause.RELOAD));
            }
        };
    }

    void unregister(Plugin plugin) {
        handlers.removeIf(h -> h.plugin == plugin);
    }

    /** Fires LifecycleEvents.COMMANDS once all plugins are enabled. */
    void fireCommands() {
        fired = true;
        run(handlers, ReloadableRegistrarEvent.Cause.INITIAL);
    }

    private void run(List<Handler> hs, ReloadableRegistrarEvent.Cause cause) {
        List<Handler> todo = new ArrayList<>();
        for (Handler h : hs) if (h.event.equals("commands") && h.plugin.isEnabled()) todo.add(h);
        todo.sort(Comparator.comparing((Handler h) -> h.monitor).thenComparingInt(h -> h.order));
        for (Handler h : todo) {
            Registrar r = new Registrar(h.plugin.getPluginMeta());
            ReloadableRegistrarEvent<Commands> ev = new ReloadableRegistrarEvent<>() {
                @Override public Commands registrar() { return r; }
                @Override public Cause cause() { return cause; }
            };
            try {
                h.handler.run(ev);
            } catch (Throwable t) {
                b.log.log(Level.SEVERE, "Could not run COMMANDS lifecycle handler of " + h.plugin.getName(), t);
            }
        }
    }

    // Registration

    final class Registrar implements Commands {
        private final PluginMeta owner;

        Registrar(PluginMeta owner) { this.owner = owner; }

        @Override public CommandDispatcher<CommandSourceStack> getDispatcher() { return dispatcher; }

        @Override
        public Set<String> register(LiteralCommandNode<CommandSourceStack> node, String description, Collection<String> aliases) {
            return register(owner, node, description, aliases);
        }

        @Override
        public Set<String> register(PluginMeta meta, LiteralCommandNode<CommandSourceStack> node, String description, Collection<String> aliases) {
            return registerWithFlags(meta, node, description, aliases, Set.of());
        }

        @Override
        public Set<String> registerWithFlags(PluginMeta meta, LiteralCommandNode<CommandSourceStack> node, String description, Collection<String> aliases, Set<CommandRegistrationFlag> flags) {
            return add(meta == null ? owner : meta, node, description, aliases);
        }

        @Override
        public Set<String> register(String label, String description, Collection<String> aliases, BasicCommand cmd) {
            return register(owner, label, description, aliases, cmd);
        }

        @Override
        public Set<String> register(PluginMeta meta, String label, String description, Collection<String> aliases, BasicCommand cmd) {
            LiteralArgumentBuilder<CommandSourceStack> lit = LiteralArgumentBuilder.<CommandSourceStack>literal(label)
                    .requires(s -> cmd.canUse(s.getSender()))
                    .executes(ctx -> {
                        cmd.execute(ctx.getSource(), new String[0]);
                        return 1;
                    });
            RequiredArgumentBuilder<CommandSourceStack, String> args = RequiredArgumentBuilder.<CommandSourceStack, String>argument("args", StringArgumentType.greedyString())
                    .suggests((ctx, sb) -> {
                        String in = sb.getInput().substring(sb.getStart());
                        String[] parts = in.split(" ", -1);
                        int last = in.lastIndexOf(' ') + 1;
                        var sub = sb.createOffset(sb.getStart() + last);
                        for (String s : cmd.suggest(ctx.getSource(), parts)) sub.suggest(s);
                        return sub.buildFuture();
                    })
                    .executes(ctx -> {
                        cmd.execute(ctx.getSource(), ctx.getArgument("args", String.class).split(" ", -1));
                        return 1;
                    });
            return add(meta == null ? owner : meta, lit.then(args).build(), description, aliases);
        }
    }

    private Set<String> add(PluginMeta meta, LiteralCommandNode<CommandSourceStack> node, String description, Collection<String> aliases) {
        String ns = meta.getName().toLowerCase(Locale.ROOT);
        String name = node.getLiteral().toLowerCase(Locale.ROOT);
        Set<String> labels = new LinkedHashSet<>();
        put(ns + ":" + name, node);
        labels.add(ns + ":" + name);
        if (claim(name, ns)) {
            put(name, node);
            labels.add(name);
        }
        List<String> als = new ArrayList<>();
        for (String a : aliases == null ? List.<String>of() : aliases) {
            String al = a.toLowerCase(Locale.ROOT);
            put(ns + ":" + al, node);
            if (claim(al, ns)) {
                put(al, node);
                labels.add(al);
                als.add(al);
            }
        }
        commands.put(ns + ":" + name, new Registered(ns, node, description, als));
        b.conn.notify(Wire.OP_REGISTER_COMMAND, meta.getName(), name, als, description == null ? "" : description, "", "", tree(node, name));
        return Set.copyOf(labels);
    }

    /** A plain label is free unless a command of another plugin holds it. */
    private boolean claim(String label, String ns) {
        CommandNode<CommandSourceStack> cur = dispatcher.getRoot().getChild(label);
        if (cur == null) return true;
        for (Registered r : commands.values()) {
            if (r.plugin.equals(ns)) continue;
            if (r.node.getLiteral().equalsIgnoreCase(label) || r.aliases.contains(label)) return false;
        }
        return true;
    }

    private void put(String label, LiteralCommandNode<CommandSourceStack> node) {
        remove(dispatcher.getRoot(), label);
        LiteralCommandNode<CommandSourceStack> copy = node;
        if (!label.equals(node.getLiteral())) {
            copy = new LiteralCommandNode<>(label, node.getCommand(), node.getRequirement(), node.getRedirect(), node.getRedirectModifier(), node.isFork());
            for (CommandNode<CommandSourceStack> c : node.getChildren()) copy.addChild(c);
        }
        dispatcher.getRoot().addChild(copy);
    }

    private static final Field[] CHILD_MAPS = childMaps();

    private static Field[] childMaps() {
        try {
            Field[] fs = {CommandNode.class.getDeclaredField("children"), CommandNode.class.getDeclaredField("literals"), CommandNode.class.getDeclaredField("arguments")};
            for (Field f : fs) f.setAccessible(true);
            return fs;
        } catch (NoSuchFieldException e) {
            throw new ExceptionInInitializerError(e);
        }
    }

    static void remove(CommandNode<?> parent, String name) {
        try {
            for (Field f : CHILD_MAPS) ((Map<?, ?>) f.get(parent)).remove(name);
        } catch (IllegalAccessException e) {
            throw new IllegalStateException(e);
        }
    }

    // Tree export for the Go command tree

    private Map<String, Object> tree(CommandNode<CommandSourceStack> node, String name) {
        Map<String, Object> m = export(node, List.of(), new java.util.IdentityHashMap<>());
        m.put("n", name);
        return m;
    }

    private Map<String, Object> export(CommandNode<CommandSourceStack> n, List<String> path, Map<CommandNode<?>, List<String>> seen) {
        Map<String, Object> m = new LinkedHashMap<>();
        List<String> here = new ArrayList<>(path);
        here.add(n.getName());
        seen.put(n, here);
        m.put("n", n.getName());
        m.put("k", n instanceof ArgumentCommandNode ? "arg" : "lit");
        if (n.getCommand() != null) m.put("x", true);
        if (path.isEmpty() && n.getRequirement() != null && !isTrivial(n)) m.put("q", true);
        if (n instanceof ArgumentCommandNode<CommandSourceStack, ?> a) {
            parser(a.getType(), m);
            if (a.getCustomSuggestions() != null) m.put("s", true);
        }
        if (n.getRedirect() != null) {
            List<String> target = n.getRedirect() instanceof RootCommandNode ? List.of() : seen.get(n.getRedirect());
            if (target != null) m.put("r", target);
        }
        List<Object> kids = new ArrayList<>();
        if (here.size() < 32) {
            for (CommandNode<CommandSourceStack> c : n.getChildren()) {
                if (seen.containsKey(c)) continue;
                kids.add(export(c, here, seen));
            }
        }
        if (!kids.isEmpty()) m.put("c", kids);
        return m;
    }

    /** The default requirement of brigadier's builders always passes. */
    private static boolean isTrivial(CommandNode<CommandSourceStack> n) {
        return n.getRequirement().getClass().getName().startsWith("com.mojang.brigadier.builder.ArgumentBuilder$$");
    }

    static void parser(ArgumentType<?> t, Map<String, Object> m) {
        switch (t) {
            case BoolArgumentType x -> m.put("p", "brigadier:bool");
            case IntegerArgumentType x -> bounds(m, "brigadier:integer", x.getMinimum(), Integer.MIN_VALUE, x.getMaximum(), Integer.MAX_VALUE);
            case LongArgumentType x -> bounds(m, "brigadier:long", x.getMinimum(), Long.MIN_VALUE, x.getMaximum(), Long.MAX_VALUE);
            case FloatArgumentType x -> bounds(m, "brigadier:float", x.getMinimum(), -Float.MAX_VALUE, x.getMaximum(), Float.MAX_VALUE);
            case DoubleArgumentType x -> bounds(m, "brigadier:double", x.getMinimum(), -Double.MAX_VALUE, x.getMaximum(), Double.MAX_VALUE);
            case StringArgumentType x -> {
                m.put("p", "brigadier:string");
                m.put("mode", x.getType().ordinal());
            }
            case BridgeArgumentProvider.Native<?> x -> {
                m.put("p", x.parser());
                m.putAll(x.props());
            }
            case CustomArgumentType<?, ?> x -> {
                parser(x.getNativeType(), m);
                m.put("s", true);
            }
            default -> {
                m.put("p", "brigadier:string");
                m.put("mode", 0);
                m.put("s", true);
            }
        }
    }

    private static void bounds(Map<String, Object> m, String p, double min, double minDef, double max, double maxDef) {
        m.put("p", p);
        if (min != minDef) m.put("min", min);
        if (max != maxDef) m.put("max", max);
    }

    // Execution and suggestions

    CommandSourceStack source(CommandSender sender) {
        return new Source(b, sender, sender instanceof Entity e ? e : null, null);
    }

    boolean owns(String line) {
        String label = line.split(" ", 2)[0].toLowerCase(Locale.ROOT);
        return dispatcher.getRoot().getChild(label) != null;
    }

    boolean canUse(CommandSender sender, String label) {
        CommandNode<CommandSourceStack> n = dispatcher.getRoot().getChild(label.toLowerCase(Locale.ROOT));
        return n != null && n.canUse(source(sender));
    }

    boolean execute(CommandSender sender, String line) {
        ParseResults<CommandSourceStack> parse = dispatcher.parse(line, source(sender));
        try {
            dispatcher.execute(parse);
        } catch (CommandSyntaxException e) {
            sender.sendMessage(error(e));
        }
        return true;
    }

    static Component error(CommandSyntaxException e) {
        Component msg = Component.text().color(NamedTextColor.RED).append(component(e.getRawMessage())).build();
        if (e.getInput() == null || e.getCursor() < 0) return msg;
        int cursor = Math.min(e.getInput().length(), e.getCursor());
        String before = e.getInput().substring(Math.max(0, cursor - CommandSyntaxException.CONTEXT_AMOUNT), cursor);
        Component ctx = Component.text().color(NamedTextColor.GRAY)
                .append(Component.text((cursor > CommandSyntaxException.CONTEXT_AMOUNT ? "..." : "") + before))
                .append(Component.text(e.getInput().substring(cursor), NamedTextColor.RED, TextDecoration.UNDERLINED))
                .append(Component.translatable("command.context.here", NamedTextColor.RED, TextDecoration.ITALIC))
                .build();
        return Component.text().append(msg).append(Component.newline()).append(ctx).build();
    }

    static Component component(Message m) {
        if (m == null) return Component.empty();
        if (m instanceof ComponentMessage c) return c.component;
        return Component.text(m.getString());
    }

    /** [start, length, [{text, tooltip}]] with UTF-16 offsets into line. */
    Object[] suggest(CommandSender sender, String line) throws Exception {
        Suggestions s;
        if (owns(line)) {
            CommandSourceStack src = source(sender);
            s = b.main.call(() -> dispatcher.getCompletionSuggestions(dispatcher.parse(line, src)), 10_000).get(10, TimeUnit.SECONDS);
        } else {
            List<String> res = b.main.call(() -> b.commandMap.tabComplete(sender, line), 10_000);
            int start = line.lastIndexOf(' ') + 1;
            List<Suggestion> out = new ArrayList<>();
            StringRange r = StringRange.between(start, line.length());
            for (String t : res == null ? List.<String>of() : res) out.add(new Suggestion(r, t));
            s = new Suggestions(r, out);
        }
        List<Object> out = new ArrayList<>();
        for (Suggestion sg : s.getList()) {
            Map<String, Object> m = new LinkedHashMap<>();
            m.put("text", sg.getText());
            if (sg.getTooltip() != null) m.put("tooltip", Text.json(component(sg.getTooltip())));
            out.add(m);
        }
        return new Object[] {s.getRange().getStart(), s.getRange().getLength(), out};
    }

    /** Bukkit's command map; every registered command is made known to Go. */
    static final class CommandMapImpl extends org.bukkit.command.SimpleCommandMap {
        CommandMapImpl(Bridge b) {
            super(b.server, new java.util.HashMap<>());
        }

        /** Bukkit's own commands (version, plugins) stay Java-only; the server has its own. */
        @Override
        public boolean register(String label, String fallbackPrefix, org.bukkit.command.Command command) {
            boolean ok = super.register(label, fallbackPrefix, command);
            Bridge b = Bridge.get();
            if (b == null || fallbackPrefix.equalsIgnoreCase("bukkit")) return ok;
            String perm = command.getPermission() == null ? "" : command.getPermission();
            String owner = command instanceof org.bukkit.command.PluginIdentifiableCommand pc ? pc.getPlugin().getName() : fallbackPrefix;
            b.conn.notify(Wire.OP_REGISTER_COMMAND, owner, label.toLowerCase(Locale.ROOT), command.getAliases(), command.getDescription(), command.getUsage(), perm, null);
            return ok;
        }
    }

    // Paper types

    /** A command source: who runs the command, as whom and where. */
    record Source(Bridge b, CommandSender sender, Entity executor, Location location) implements CommandSourceStack {
        @Override
        public Location getLocation() {
            if (location != null) return location.clone();
            if (executor != null) return executor.getLocation();
            List<World> ws = b.worlds.all();
            return ws.isEmpty() ? new Location(null, 0, 0, 0) : ws.getFirst().getSpawnLocation();
        }

        @Override public CommandSender getSender() { return sender; }
        @Override public Entity getExecutor() { return executor; }

        @Override
        public Player getPlayerOrThrow() throws CommandSyntaxException {
            if (executor instanceof Player p) return p;
            throw NOT_PLAYER.create();
        }

        @Override
        public Entity getEntityOrThrow() throws CommandSyntaxException {
            if (executor != null) return executor;
            throw NOT_ENTITY.create();
        }

        @Override public CommandSourceStack withLocation(Location l) { return new Source(b, sender, executor, l); }
        @Override public CommandSourceStack withExecutor(Entity e) { return new Source(b, sender, e, location); }
        @Override public void sendReply(ComponentLike m) { sender.sendMessage(m); }
        @Override public void sendSuccess(ComponentLike m, boolean inform) { sender.sendMessage(m); }
        @Override public void sendFailure(ComponentLike m) { sender.sendMessage(Component.text().color(NamedTextColor.RED).append(m)); }
    }

    /** A brigadier message carrying an Adventure component. */
    record ComponentMessage(Component component) implements Message, ComponentLike {
        @Override public String getString() { return PlainTextComponentSerializer.plainText().serialize(component); }
        @Override public Component asComponent() { return component; }
    }

    /** Loaded through META-INF/services. */
    public static final class Messages implements MessageComponentSerializer {
        public Messages() {}

        @Override public Component deserialize(Message m) { return component(m); }
        @Override public Message serialize(Component c) { return new ComponentMessage(c); }
    }
}
