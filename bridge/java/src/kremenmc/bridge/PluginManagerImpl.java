package kremenmc.bridge;

import io.papermc.paper.plugin.PermissionManager;
import io.papermc.paper.plugin.configuration.PluginMeta;
import io.papermc.paper.plugin.provider.entrypoint.DependencyContext;
import java.io.File;
import java.io.InputStream;
import java.lang.invoke.MethodHandle;
import java.lang.invoke.MethodHandles;
import java.lang.invoke.MethodType;
import java.lang.reflect.Method;
import java.lang.reflect.Modifier;
import java.util.ArrayList;
import java.util.Collection;
import java.util.HashSet;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.Set;
import java.util.concurrent.ConcurrentHashMap;
import java.util.jar.JarFile;
import java.util.logging.Level;
import java.util.zip.ZipEntry;
import org.bukkit.command.Command;
import org.bukkit.command.PluginCommandYamlParser;
import org.bukkit.event.Event;
import org.bukkit.event.EventException;
import org.bukkit.event.EventHandler;
import org.bukkit.event.EventPriority;
import org.bukkit.event.HandlerList;
import org.bukkit.event.Listener;
import org.bukkit.permissions.Permissible;
import org.bukkit.permissions.Permission;
import org.bukkit.plugin.EventExecutor;
import org.bukkit.plugin.IllegalPluginAccessException;
import org.bukkit.plugin.InvalidDescriptionException;
import org.bukkit.plugin.InvalidPluginException;
import org.bukkit.plugin.Plugin;
import org.bukkit.plugin.PluginDescriptionFile;
import org.bukkit.plugin.PluginLoader;
import org.bukkit.plugin.PluginManager;
import org.bukkit.plugin.RegisteredListener;
import org.bukkit.plugin.java.JavaPlugin;
import org.bukkit.plugin.java.PluginClassLoader;
import org.yaml.snakeyaml.Yaml;

/** PluginManager with Bukkit loading semantics on top of Paper's PluginClassLoader. */
final class PluginManagerImpl implements PluginManager {
    private final Bridge b;
    private final Map<String, Plugin> plugins = new LinkedHashMap<>();
    private final Map<String, Permission> permissions = new ConcurrentHashMap<>();

    PluginManagerImpl(Bridge b) { this.b = b; }

    // Loading

    static PluginDescriptionFile describe(File jar) throws InvalidDescriptionException {
        try (JarFile jf = new JarFile(jar)) {
            ZipEntry paper = jf.getEntry("paper-plugin.yml");
            ZipEntry bukkit = jf.getEntry("plugin.yml");
            if (bukkit != null) {
                try (InputStream in = jf.getInputStream(bukkit)) {
                    return new PluginDescriptionFile(in);
                }
            }
            if (paper == null) throw new InvalidDescriptionException("Jar does not contain plugin.yml or paper-plugin.yml");
            try (InputStream in = jf.getInputStream(paper)) {
                Map<?, ?> m = new Yaml().load(in);
                Map<String, Object> d = new LinkedHashMap<>();
                for (String k : List.of("name", "version", "main", "api-version", "description", "website", "author", "authors", "prefix", "folia-supported")) {
                    if (m.get(k) != null) d.put(k, m.get(k));
                }
                if (m.get("dependencies") instanceof Map<?, ?> deps && deps.get("server") instanceof Map<?, ?> server) {
                    List<String> depend = new ArrayList<>(), soft = new ArrayList<>(), before = new ArrayList<>();
                    for (Map.Entry<?, ?> e : server.entrySet()) {
                        Map<?, ?> spec = e.getValue() instanceof Map<?, ?> s ? s : Map.of();
                        String name = String.valueOf(e.getKey());
                        String load = String.valueOf(spec.get("load") == null ? "OMIT" : spec.get("load")).toUpperCase(Locale.ROOT);
                        boolean required = !Boolean.FALSE.equals(spec.get("required"));
                        if (load.equals("BEFORE")) before.add(name);
                        else if (required) depend.add(name);
                        else soft.add(name);
                    }
                    d.put("depend", depend);
                    d.put("softdepend", soft);
                    d.put("loadbefore", before);
                }
                return new PluginDescriptionFile(new java.io.StringReader(new Yaml().dump(d)));
            }
        } catch (InvalidDescriptionException e) {
            throw e;
        } catch (Exception e) {
            throw new InvalidDescriptionException(e, "cannot read descriptor of " + jar);
        }
    }

    @Override
    public Plugin loadPlugin(File jar) throws InvalidPluginException, InvalidDescriptionException {
        PluginDescriptionFile desc = describe(jar);
        synchronized (plugins) {
            if (plugins.containsKey(desc.getName())) throw new InvalidPluginException("Duplicate plugin " + desc.getName());
        }
        for (String dep : desc.getDepend()) {
            if (getPlugin(dep) == null) throw new InvalidPluginException("Unknown dependency " + dep + " of " + desc.getName());
        }
        if (!desc.getLibraries().isEmpty()) {
            b.log.warning(desc.getName() + " declares libraries " + desc.getLibraries() + "; library loading is not supported by the kremenmc bridge");
        }
        File dataFolder = new File(jar.getParentFile(), desc.getName());
        PluginClassLoader loader;
        try {
            loader = new PluginClassLoader(getClass().getClassLoader(), desc, dataFolder, jar, null, new JarFile(jar), dependencyContext(desc));
        } catch (InvalidPluginException e) {
            throw e;
        } catch (Throwable t) {
            throw new InvalidPluginException(t);
        }
        JavaPlugin plugin = loader.getPlugin();
        synchronized (plugins) {
            plugins.put(desc.getName(), plugin);
        }
        b.log.info("Loading " + desc.getFullName());
        plugin.onLoad();
        return plugin;
    }

    private DependencyContext dependencyContext(PluginDescriptionFile desc) {
        return new DependencyContext() {
            @Override
            public boolean isTransitiveDependency(PluginMeta a, PluginMeta c) {
                return PluginManagerImpl.this.isTransitiveDependency(a, c);
            }

            @Override
            public boolean hasDependency(String name) {
                return desc.getDepend().contains(name) || desc.getSoftDepend().contains(name);
            }
        };
    }

    @Override
    public Plugin[] loadPlugins(File dir) {
        File[] jars = dir.listFiles((d, n) -> n.endsWith(".jar"));
        return jars == null ? new Plugin[0] : loadPlugins(jars);
    }

    @Override
    public Plugin[] loadPlugins(File[] jars) {
        List<Plugin> out = new ArrayList<>();
        for (File f : jars) {
            try {
                out.add(loadPlugin(f));
            } catch (Throwable t) {
                b.log.log(Level.SEVERE, "Could not load '" + f.getPath() + "'", t);
            }
        }
        return out.toArray(new Plugin[0]);
    }

    @Override
    public Plugin getPlugin(String name) {
        synchronized (plugins) {
            Plugin p = plugins.get(name);
            if (p != null) return p;
            for (Plugin q : plugins.values()) if (q.getName().equalsIgnoreCase(name) || q.getPluginMeta().getProvidedPlugins().contains(name)) return q;
            return null;
        }
    }

    @Override
    public Plugin[] getPlugins() {
        synchronized (plugins) {
            return plugins.values().toArray(new Plugin[0]);
        }
    }

    @Override public boolean isPluginEnabled(String name) { Plugin p = getPlugin(name); return p != null && p.isEnabled(); }
    @Override public boolean isPluginEnabled(Plugin p) { return p != null && p.isEnabled(); }

    @Override
    public void enablePlugin(Plugin plugin) {
        if (plugin.isEnabled()) return;
        List<Command> cmds = PluginCommandYamlParser.parse(plugin);
        if (!cmds.isEmpty()) b.commandMap.registerAll(plugin.getName().toLowerCase(Locale.ROOT), cmds);
        for (Permission p : plugin.getDescription().getPermissions()) {
            if (!permissions.containsKey(p.getName().toLowerCase(Locale.ROOT))) addPermission(p);
        }
        b.log.info("Enabling " + plugin.getPluginMeta().getDisplayName());
        try {
            ((JavaPlugin) plugin).setEnabled(true);
        } catch (Throwable t) {
            b.log.log(Level.SEVERE, "Error occurred while enabling " + plugin.getPluginMeta().getDisplayName(), t);
            state(plugin, "failed");
            disablePlugin(plugin);
            return;
        }
        state(plugin, plugin.isEnabled() ? "enabled" : "disabled");
        listenersChanged();
    }

    @Override
    public void disablePlugin(Plugin plugin) {
        if (plugin.isEnabled()) {
            b.log.info("Disabling " + plugin.getPluginMeta().getDisplayName());
            try {
                ((JavaPlugin) plugin).setEnabled(false);
            } catch (Throwable t) {
                b.log.log(Level.SEVERE, "Error occurred while disabling " + plugin.getPluginMeta().getDisplayName(), t);
            }
            state(plugin, "disabled");
        }
        b.scheduler.cancelTasks(plugin);
        b.brigadier.unregister(plugin);
        b.services.unregisterAll(plugin);
        b.messenger.unregisterIncomingPluginChannel(plugin);
        b.messenger.unregisterOutgoingPluginChannel(plugin);
        HandlerList.unregisterAll(plugin);
        listenersChanged();
    }

    private void state(Plugin p, String state) {
        b.conn.notify(Wire.OP_PLUGIN_STATE, p.getName(), state, p.getPluginMeta().getVersion());
    }

    @Override
    public void disablePlugins() {
        Plugin[] ps = getPlugins();
        for (int i = ps.length - 1; i >= 0; i--) disablePlugin(ps[i]);
    }

    @Override
    public void clearPlugins() {
        disablePlugins();
        synchronized (plugins) {
            plugins.clear();
        }
        HandlerList.unregisterAll();
        permissions.clear();
    }

    @Override public void registerInterface(Class<? extends PluginLoader> loader) {}
    @Override public boolean useTimings() { return false; }

    @Override
    public boolean isTransitiveDependency(PluginMeta plugin, PluginMeta depend) {
        Set<String> seen = new HashSet<>();
        List<String> todo = new ArrayList<>(plugin.getPluginDependencies());
        todo.addAll(plugin.getPluginSoftDependencies());
        while (!todo.isEmpty()) {
            String n = todo.removeLast();
            if (n.equals(depend.getName())) return true;
            if (!seen.add(n)) continue;
            Plugin p = getPlugin(n);
            if (p != null) {
                todo.addAll(p.getPluginMeta().getPluginDependencies());
                todo.addAll(p.getPluginMeta().getPluginSoftDependencies());
            }
        }
        return false;
    }

    @Override
    public void overridePermissionManager(Plugin plugin, PermissionManager mgr) {
        b.log.warning(plugin.getName() + " tried to override the permission manager; not supported by the kremenmc bridge");
    }

    // Events

    @Override
    public void callEvent(Event event) {
        if (event.isAsynchronous() && b.main.isCurrent()) {
            throw new IllegalStateException(event.getEventName() + " may only be triggered asynchronously.");
        }
        if (!event.isAsynchronous() && !b.main.isCurrent()) {
            throw new IllegalStateException(event.getEventName() + " may only be triggered synchronously.");
        }
        for (RegisteredListener rl : event.getHandlers().getRegisteredListeners()) {
            if (!rl.getPlugin().isEnabled()) continue;
            try {
                rl.callEvent(event);
            } catch (Throwable t) {
                Throwable cause = t instanceof EventException && t.getCause() != null ? t.getCause() : t;
                b.log.log(Level.SEVERE, "Could not pass event " + event.getEventName() + " to " + rl.getPlugin().getPluginMeta().getDisplayName(), cause);
            }
        }
    }

    @Override
    public void registerEvents(Listener listener, Plugin plugin) {
        if (!plugin.isEnabled()) throw new IllegalPluginAccessException("Plugin attempted to register " + listener + " while not enabled");
        for (Method m : handlerMethods(listener.getClass())) {
            EventHandler eh = m.getAnnotation(EventHandler.class);
            Class<?> param = m.getParameterTypes()[0];
            if (!Event.class.isAssignableFrom(param)) {
                b.log.severe(plugin.getName() + " attempted to register an invalid EventHandler method signature \"" + m.toGenericString() + "\" in " + listener.getClass());
                continue;
            }
            Class<? extends Event> eventClass = param.asSubclass(Event.class);
            HandlerList handlers;
            try {
                handlers = handlerList(eventClass);
            } catch (IllegalPluginAccessException e) {
                b.log.severe(plugin.getName() + ": " + e.getMessage());
                continue;
            }
            handlers.register(new RegisteredListener(listener, executor(m, eventClass), eh.priority(), plugin, eh.ignoreCancelled()));
        }
        listenersChanged();
    }

    private static List<Method> handlerMethods(Class<?> c) {
        Set<Method> out = new HashSet<>();
        for (Method m : c.getMethods()) if (m.isAnnotationPresent(EventHandler.class)) out.add(m);
        for (Class<?> k = c; k != null && k != Object.class; k = k.getSuperclass()) {
            for (Method m : k.getDeclaredMethods()) {
                if (m.isAnnotationPresent(EventHandler.class) && !m.isBridge() && !m.isSynthetic()) out.add(m);
            }
        }
        List<Method> list = new ArrayList<>();
        for (Method m : out) if (m.getParameterCount() == 1 && !Modifier.isStatic(m.getModifiers())) list.add(m);
        return list;
    }

    private static EventExecutor executor(Method m, Class<? extends Event> eventClass) {
        MethodHandle mh;
        try {
            m.setAccessible(true);
            mh = MethodHandles.lookup().unreflect(m).asType(MethodType.methodType(void.class, Object.class, Event.class));
        } catch (ReflectiveOperationException | RuntimeException e) {
            throw new IllegalStateException("cannot access event handler " + m, e);
        }
        return (listener, event) -> {
            if (!eventClass.isInstance(event)) return;
            try {
                mh.invokeExact((Object) listener, event);
            } catch (Throwable t) {
                throw new EventException(t);
            }
        };
    }

    static HandlerList handlerList(Class<? extends Event> c) {
        for (Class<?> k = c; k != null && Event.class.isAssignableFrom(k); k = k.getSuperclass()) {
            try {
                Method m = k.getDeclaredMethod("getHandlerList");
                m.setAccessible(true);
                return (HandlerList) m.invoke(null);
            } catch (NoSuchMethodException e) {
                // try superclass
            } catch (ReflectiveOperationException e) {
                throw new IllegalPluginAccessException("cannot get handler list of " + c.getName() + ": " + e);
            }
        }
        throw new IllegalPluginAccessException("Unable to find handler list for event " + c.getName() + ". Static getHandlerList method required!");
    }

    @Override
    public void registerEvent(Class<? extends Event> event, Listener listener, EventPriority priority, EventExecutor executor, Plugin plugin) {
        registerEvent(event, listener, priority, executor, plugin, false);
    }

    @Override
    public void registerEvent(Class<? extends Event> event, Listener listener, EventPriority priority, EventExecutor executor, Plugin plugin, boolean ignoreCancelled) {
        if (!plugin.isEnabled()) throw new IllegalPluginAccessException("Plugin attempted to register " + event + " while not enabled");
        handlerList(event).register(new RegisteredListener(listener, executor, priority, plugin, ignoreCancelled));
        listenersChanged();
    }

    void listenersChanged() {
        b.conn.notify(Wire.OP_LISTENERS, Events.listenedNames());
    }

    // Permissions

    @Override public Permission getPermission(String name) { return permissions.get(name.toLowerCase(Locale.ROOT)); }

    @Override
    public void addPermission(Permission perm) {
        if (permissions.putIfAbsent(perm.getName().toLowerCase(Locale.ROOT), perm) != null) {
            throw new IllegalArgumentException("The permission " + perm.getName().toLowerCase(Locale.ROOT) + " is already defined!");
        }
    }

    @Override public void addPermissions(List<Permission> perms) { for (Permission p : perms) addPermission(p); }
    @Override public void removePermission(Permission perm) { removePermission(perm.getName()); }
    @Override public void removePermission(String name) { permissions.remove(name.toLowerCase(Locale.ROOT)); }
    @Override public void clearPermissions() { permissions.clear(); }
    @Override public Set<Permission> getPermissions() { return new HashSet<>(permissions.values()); }
    @Override public Set<Permission> getDefaultPermissions(boolean op) { return new HashSet<>(); }
    @Override public void recalculatePermissionDefaults(Permission perm) {}
    @Override public void subscribeToPermission(String permission, Permissible permissible) {}
    @Override public void unsubscribeFromPermission(String permission, Permissible permissible) {}
    @Override public Set<Permissible> getPermissionSubscriptions(String permission) { return new HashSet<>(); }
    @Override public void subscribeToDefaultPerms(boolean op, Permissible permissible) {}
    @Override public void unsubscribeFromDefaultPerms(boolean op, Permissible permissible) {}
    @Override public Set<Permissible> getDefaultPermSubscriptions(boolean op) { return new HashSet<>(); }

    Collection<Plugin> all() {
        synchronized (plugins) {
            return new ArrayList<>(plugins.values());
        }
    }
}
