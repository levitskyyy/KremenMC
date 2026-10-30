package kremenmc.bridge;

import io.papermc.paper.registry.RegistryKey;
import java.lang.reflect.Field;
import java.lang.reflect.Modifier;
import java.lang.reflect.ParameterizedType;
import java.lang.reflect.Type;
import java.util.HashMap;
import java.util.Iterator;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.stream.Stream;
import org.bukkit.Keyed;
import org.bukkit.NamespacedKey;
import org.bukkit.Registry;

/**
 * Paper's registry access. The Go server owns the real registry contents, so
 * a registry answers every key with a value of its element type: the enum
 * constant of that name, or a proxy that knows its key, name and ordinal.
 */
final class Registries {
    private Registries() {}

    private static final Map<Object, Registry<?>> REGISTRIES = new ConcurrentHashMap<>();
    private static volatile Map<RegistryKey<?>, Class<?>> elementTypes;

    /** Loaded through META-INF/services. */
    public static final class Access implements io.papermc.paper.registry.RegistryAccess {
        @Override
        public <T extends Keyed> Registry<T> getRegistry(Class<T> type) { return registry(type); }

        @Override
        @SuppressWarnings("unchecked")
        public <T extends Keyed> Registry<T> getRegistry(RegistryKey<T> key) {
            return (Registry<T>) registry((Class<Keyed>) elementType(key));
        }
    }

    @SuppressWarnings("unchecked")
    static <T extends Keyed> Registry<T> registry(Class<T> type) {
        return (Registry<T>) REGISTRIES.computeIfAbsent(type, t -> Stubs.proxy(Registry.class, new RegistryImpl<>(type)));
    }

    private static Class<?> elementType(RegistryKey<?> key) {
        Map<RegistryKey<?>, Class<?>> m = elementTypes;
        if (m == null) {
            m = new HashMap<>();
            for (Field f : RegistryKey.class.getFields()) {
                if (!Modifier.isStatic(f.getModifiers()) || f.getType() != RegistryKey.class) continue;
                if (!(f.getGenericType() instanceof ParameterizedType pt)) continue;
                Class<?> c = raw(pt.getActualTypeArguments()[0]);
                try {
                    if (c != null) m.put((RegistryKey<?>) f.get(null), c);
                } catch (IllegalAccessException ignored) {
                }
            }
            elementTypes = m;
        }
        return m.getOrDefault(key, Keyed.class);
    }

    private static Class<?> raw(Type t) {
        if (t instanceof Class<?> c) return c;
        if (t instanceof ParameterizedType p) return raw(p.getRawType());
        return null;
    }

    public static final class RegistryImpl<T extends Keyed> {
        private final Class<T> type;
        private final Map<NamespacedKey, T> values = new ConcurrentHashMap<>();
        private final AtomicInteger ordinals = new AtomicInteger();

        RegistryImpl(Class<T> type) {
            this.type = type;
            if (type.isEnum()) {
                for (T c : type.getEnumConstants()) {
                    values.put(NamespacedKey.minecraft(((Enum<?>) c).name().toLowerCase(Locale.ROOT)), c);
                }
            }
        }

        public T get(NamespacedKey key) {
            if (key == null) return null;
            if (type.isEnum()) return values.get(key);
            if (!type.isInterface() && !java.lang.reflect.Modifier.isAbstract(type.getModifiers())) return null;
            return values.computeIfAbsent(key, k -> {
                Object e = type == org.bukkit.inventory.ItemType.class ? new Items.TypeImpl(k, ordinals.getAndIncrement()) : new Element(k, type, ordinals.getAndIncrement());
                if (type == org.bukkit.inventory.MenuType.class) return type.cast(Stubs.proxy(org.bukkit.inventory.MenuType.Typed.class, e));
                return type.isInterface() ? Stubs.proxy(type, e) : Stubs.subclass(type, e);
            });
        }

        public T get(net.kyori.adventure.key.Key key) {
            return key == null ? null : get(new NamespacedKey(key.namespace(), key.value()));
        }

        public NamespacedKey getKey(T value) { return value.getKey(); }
        public Iterator<T> iterator() { return List.copyOf(values.values()).iterator(); }
        public Stream<T> stream() { return List.copyOf(values.values()).stream(); }
        public Stream<NamespacedKey> keyStream() { return List.copyOf(values.keySet()).stream(); }
        public int size() { return values.size(); }

        public T match(String input) {
            NamespacedKey k = NamespacedKey.fromString(input.toLowerCase(Locale.ROOT).replace(' ', '_'));
            return k == null ? null : get(k);
        }

        @Override public String toString() { return "Registry{" + type.getSimpleName() + "}"; }
    }

    /** A registry value the bridge knows only by key. */
    public static final class Element {
        private final NamespacedKey key;
        private final Class<?> type;
        private final int ordinal;

        Element(NamespacedKey key, Class<?> type, int ordinal) {
            this.key = key;
            this.type = type;
            this.ordinal = ordinal;
        }

        public NamespacedKey getKey() { return key; }
        public NamespacedKey getKeyOrThrow() { return key; }
        public NamespacedKey getKeyOrNull() { return key; }
        public boolean isRegistered() { return true; }
        public net.kyori.adventure.key.Key key() { return key; }
        public String name() { return key.getKey().toUpperCase(Locale.ROOT).replace('.', '_').replace('/', '_'); }
        public int ordinal() { return ordinal; }
        public String getTranslationKey() { return translationKey(); }
        public String getName() { return name(); }
        public int getId() { return ordinal; }

        public String translationKey() {
            return type.getSimpleName().toLowerCase(Locale.ROOT) + "." + key.getNamespace() + "." + key.getKey().replace('/', '.');
        }

        public int compareTo(Object o) {
            return Stubs.implOf(o) instanceof Element e ? Integer.compare(ordinal, e.ordinal) : 0;
        }

        @Override public boolean equals(Object o) { return o instanceof Element e && e.type == type && e.key.equals(key); }
        @Override public int hashCode() { return key.hashCode() * 31 + type.hashCode(); }
        @Override public String toString() { return key.toString(); }
    }
}
