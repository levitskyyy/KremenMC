package kremenmc.bridge;

import java.lang.reflect.Array;
import java.lang.reflect.InvocationHandler;
import java.lang.reflect.InvocationTargetException;
import java.lang.reflect.Method;
import java.lang.reflect.Proxy;
import java.util.ArrayList;
import java.util.Collection;
import java.util.HashMap;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import java.util.Set;
import java.util.concurrent.ConcurrentHashMap;
import java.util.logging.Logger;
import java.util.stream.Stream;
import org.bukkit.craftbukkit.entity.CraftPlayer;
import org.bukkit.entity.Player;

/**
 * Dynamic implementations of large Bukkit interfaces. A call goes to the
 * impl object's public method with the same name and parameter types if one
 * exists, else to the interface's default method, else to a fallback: void
 * methods are no-ops, primitives return zero/false, collection-like types
 * return empty values, and everything else throws
 * UnsupportedOperationException. Each fallback is logged once per method.
 */
final class Stubs {
    private Stubs() {}

    private static final Logger LOG = Logger.getLogger("kremenmc-bridge");
    private static final Set<String> warned = ConcurrentHashMap.newKeySet();
    private static final Map<Class<?>, Map<Method, Method>> targets = new ConcurrentHashMap<>();
    private static final Method NONE;

    static {
        try {
            NONE = Object.class.getMethod("hashCode");
        } catch (NoSuchMethodException e) {
            throw new ExceptionInInitializerError(e);
        }
    }

    /** Public so generated subclasses in other packages can implement it. */
    public interface Backed {
        Object impl();
    }

    /** Interfaces whose instances must also be of a server implementation class. */
    private static final Map<Class<?>, Class<?>> BASES = Map.of(
            Player.class, CraftPlayer.class,
            org.bukkit.Server.class, org.bukkit.craftbukkit.AbstractCraftServer.class,
            org.bukkit.World.class, org.bukkit.craftbukkit.CraftWorld.class);

    /** Generates the server implementation classes ahead of the first player join. */
    static void prepare() {
        for (Class<?> base : BASES.values()) ClassProxy.prepare(base, Backed.class);
    }

    @SuppressWarnings("unchecked")
    static <T> T proxy(Class<T> iface, Object impl) {
        Handler h = new Handler(iface, impl);
        Class<?> base = BASES.get(iface);
        if (base != null) return (T) ClassProxy.create(base, h, Backed.class);
        return (T) Proxy.newProxyInstance(Stubs.class.getClassLoader(), new Class<?>[] {iface, Backed.class}, h);
    }

    /** Like proxy, for an abstract API class with a no-argument constructor. */
    @SuppressWarnings("unchecked")
    static <T> T subclass(Class<T> base, Object impl) {
        return (T) ClassProxy.create(base, new Handler(base, impl), Backed.class);
    }

    static Object implOf(Object proxy) {
        return proxy instanceof Backed b ? b.impl() : null;
    }

    private record Handler(Class<?> iface, Object impl) implements InvocationHandler {
        @Override
        public Object invoke(Object proxy, Method m, Object[] args) throws Throwable {
            if (m.getDeclaringClass() == Backed.class) return impl;
            if (m.getDeclaringClass() == Object.class) {
                return switch (m.getName()) {
                    case "equals" -> args[0] != null && (args[0] == proxy || impl.equals(implOf(args[0])));
                    case "hashCode" -> impl.hashCode();
                    default -> impl.toString();
                };
            }
            Method target = targets.computeIfAbsent(impl.getClass(), k -> new ConcurrentHashMap<>())
                    .computeIfAbsent(m, k -> resolve(impl.getClass(), k));
            if (target != NONE) {
                try {
                    return target.invoke(impl, args);
                } catch (InvocationTargetException e) {
                    throw e.getCause();
                }
            }
            if (m.isDefault()) {
                if (Proxy.isProxyClass(proxy.getClass())) return InvocationHandler.invokeDefault(proxy, m, args);
                return ClassProxy.invokeSuper(proxy, m, args);
            }
            return fallback(iface, m);
        }
    }

    private static Method resolve(Class<?> implClass, Method m) {
        try {
            Method t = implClass.getMethod(m.getName(), m.getParameterTypes());
            if (t.getDeclaringClass() == Object.class) return NONE;
            t.setAccessible(true);
            return t;
        } catch (NoSuchMethodException e) {
            return NONE;
        }
    }

    static Object fallback(Class<?> iface, Method m) {
        Class<?> r = m.getReturnType();
        String name = iface.getSimpleName() + "." + m.getName();
        Object value;
        if (r == void.class) value = null;
        else if (r == boolean.class) value = false;
        else if (r == int.class) value = 0;
        else if (r == long.class) value = 0L;
        else if (r == double.class) value = 0d;
        else if (r == float.class) value = 0f;
        else if (r == short.class) value = (short) 0;
        else if (r == byte.class) value = (byte) 0;
        else if (r == char.class) value = (char) 0;
        else if (r == Optional.class) value = Optional.empty();
        else if (r == Stream.class) value = Stream.empty();
        else if (r == List.class || r == Collection.class || r == Iterable.class) value = new ArrayList<>();
        else if (r == Set.class) value = new HashSet<>();
        else if (r == Map.class) value = new HashMap<>();
        else if (r.isArray()) value = Array.newInstance(r.getComponentType(), 0);
        else {
            if (warned.add(name)) LOG.warning("kremenmc bridge: " + name + " is not implemented");
            throw new UnsupportedOperationException("kremenmc bridge: " + name + " is not implemented");
        }
        if (warned.add(name)) LOG.info("kremenmc bridge: " + name + " is a stub returning a default value");
        return value;
    }
}
