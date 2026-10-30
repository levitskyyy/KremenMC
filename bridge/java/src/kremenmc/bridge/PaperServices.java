package kremenmc.bridge;

import io.papermc.paper.InternalAPIBridge;
import io.papermc.paper.command.brigadier.CommandSourceStack;
import io.papermc.paper.datacomponent.item.ResolvableProfile;
import io.papermc.paper.entity.poi.PoiType;
import io.papermc.paper.persistence.PersistentDataKey;
import io.papermc.paper.plugin.lifecycle.event.LifecycleEventManager;
import io.papermc.paper.plugin.provider.classloader.ClassLoaderAccess;
import io.papermc.paper.plugin.provider.classloader.ConfiguredPluginClassLoader;
import io.papermc.paper.plugin.provider.classloader.PaperClassLoaderStorage;
import io.papermc.paper.plugin.provider.classloader.PluginClassLoaderGroup;
import io.papermc.paper.world.damagesource.CombatEntry;
import io.papermc.paper.world.damagesource.FallLocationType;
import java.util.List;
import java.util.Set;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.function.BooleanSupplier;
import java.util.function.Function;
import java.util.function.Predicate;
import java.util.logging.Logger;
import net.kyori.adventure.key.Key;
import net.kyori.adventure.text.Component;
import net.kyori.adventure.text.flattener.ComponentFlattener;
import org.bukkit.GameRule;
import org.bukkit.NamespacedKey;
import org.bukkit.Statistic;
import org.bukkit.attribute.Attributable;
import org.bukkit.block.Biome;
import org.bukkit.command.CommandSender;
import org.bukkit.damage.DamageEffect;
import org.bukkit.damage.DamageSource;
import org.bukkit.damage.DamageType;
import org.bukkit.enchantments.EnchantmentOffer;
import org.bukkit.entity.Entity;
import org.bukkit.entity.EntityType;
import org.bukkit.entity.LivingEntity;
import org.bukkit.entity.Pose;
import org.bukkit.entity.SpawnCategory;
import org.bukkit.inventory.ItemStack;
import org.bukkit.persistence.PersistentDataType;
import org.bukkit.plugin.Plugin;
import org.bukkit.plugin.java.JavaPlugin;
import org.bukkit.plugin.java.PluginClassLoader;

/** ServiceLoader providers for Paper's internal hooks (see resources/META-INF/services). */
final class PaperServices {
    private PaperServices() {}

    private static UnsupportedOperationException unsupported(String what) {
        return new UnsupportedOperationException("kremenmc bridge: " + what + " requires the vanilla server and is not available");
    }

    /** Spigot-style global class sharing: every plugin can see classes of every other plugin. */
    public static final class ClassLoaders implements PaperClassLoaderStorage {
        static final List<ConfiguredPluginClassLoader> LOADERS = new CopyOnWriteArrayList<>();

        private static final PluginClassLoaderGroup GLOBAL = new PluginClassLoaderGroup() {
            @Override
            public Class<?> getClassByName(String name, boolean resolve, ConfiguredPluginClassLoader requester) {
                for (ConfiguredPluginClassLoader l : LOADERS) {
                    if (l == requester) continue;
                    try {
                        return l.loadClass(name, resolve, false, true);
                    } catch (ClassNotFoundException | LinkageError ignored) {
                    }
                }
                return null;
            }

            @Override public void remove(ConfiguredPluginClassLoader l) { LOADERS.remove(l); }
            @Override public void add(ConfiguredPluginClassLoader l) { if (!LOADERS.contains(l)) LOADERS.add(l); }
            @Override public ClassLoaderAccess getAccess() { return l -> true; }
        };

        public ClassLoaders() {}

        @Override public PluginClassLoaderGroup registerSpigotGroup(PluginClassLoader l) { GLOBAL.add(l); return GLOBAL; }
        @Override public PluginClassLoaderGroup registerOpenGroup(ConfiguredPluginClassLoader l) { GLOBAL.add(l); return GLOBAL; }
        @Override public PluginClassLoaderGroup registerAccessBackedGroup(ConfiguredPluginClassLoader l, ClassLoaderAccess a) { GLOBAL.add(l); return GLOBAL; }
        @Override public void unregisterClassloader(ConfiguredPluginClassLoader l) { LOADERS.remove(l); }
        @Override public boolean registerUnsafePlugin(ConfiguredPluginClassLoader l) { GLOBAL.add(l); return true; }
    }

    public static final class Internal implements InternalAPIBridge {
        public Internal() {}

        @Override
        @SuppressWarnings("unchecked")
        public LifecycleEventManager<Plugin> createPluginLifecycleEventManager(JavaPlugin plugin, BooleanSupplier registrationCheck) {
            return Bridge.get().brigadier.manager(plugin);
        }

        @Override public Biome constructLegacyCustomBiome() { throw unsupported("custom biomes"); }
        @Override public CombatEntry createCombatEntry(LivingEntity e, DamageSource s, float d) { throw unsupported("combat tracking"); }
        @Override public CombatEntry createCombatEntry(DamageSource s, float d, FallLocationType f, float dist) { throw unsupported("combat tracking"); }
        @Override public Predicate<CommandSourceStack> restricted(Predicate<CommandSourceStack> p, boolean r) { return p; }
        @Override public ResolvableProfile defaultMannequinProfile() { throw unsupported("mannequins"); }
        @Override public com.destroystokyo.paper.SkinParts.Mutable allSkinParts() { throw unsupported("skin parts"); }
        @Override public Component defaultMannequinDescription() { return Component.empty(); }
        @Override public <M, L> GameRule<L> legacyGameRuleBridge(GameRule<M> r, Function<L, M> a, Function<M, L> b, Class<L> c) { throw unsupported("game rules"); }
        @Override public Set<Pose> validMannequinPoses() { return Set.of(); }
        @Override public PoiType.Occupancy createOccupancy(String s) { throw unsupported("POI types"); }
        @Override public DamageSource.Builder createDamageSourceBuilder(DamageType t) { throw unsupported("damage sources"); }
        @Override public DamageEffect getDamageEffect(String s) { throw unsupported("damage effects"); }
        @Override public String getTranslationKey(EntityType t) { return "entity.minecraft." + t.name().toLowerCase(java.util.Locale.ROOT); }
        @Override public SpawnCategory getSpawnCategory(EntityType t) { return SpawnCategory.MISC; }
        @Override public ItemStack deserializeItem(byte[] data) { throw unsupported("item serialization"); }
        @Override public boolean hasDefaultEntityAttributes(NamespacedKey k) { return false; }
        @Override public Attributable getDefaultEntityAttributes(NamespacedKey k) { throw unsupported("entity attributes"); }
        @Override public String getStatisticCriteriaKey(Statistic s) { return s.name().toLowerCase(java.util.Locale.ROOT); }
        @Override public ItemStack createEmptyStack() { return Items.empty(); }
        @Override public Component resolveWithContext(Component c, CommandSender s, Entity e, boolean b) { return c; }
        @Override public ComponentFlattener componentFlattener() { return ComponentFlattener.basic(); }
        @Override public <C> PersistentDataKey<C> createPersistentDataKey(Key k, PersistentDataType<?, C> t) { throw unsupported("persistent data"); }
        @Override public EnchantmentOffer[] rollEnchantmentOffers(ItemStack i, int a, int b, int c) { throw unsupported("enchanting"); }
    }

    public static final class BuildInfo implements io.papermc.paper.ServerBuildInfo {
        public BuildInfo() {}

        private static String mc() {
            Bridge b = Bridge.get();
            return b == null ? "26.3" : b.infoString("minecraftVersion", "26.3");
        }

        private static String version() {
            Bridge b = Bridge.get();
            return b == null ? "dev" : b.infoString("version", "dev");
        }

        @Override public Key brandId() { return Key.key("kremenmc", "kremenmc"); }
        @Override public boolean isBrandCompatible(Key brand) { return brand.equals(brandId()) || brand.equals(BRAND_PAPER_ID); }
        @Override public String brandName() { return "KremenMC"; }
        @Override public String minecraftVersionId() { return mc(); }
        @Override public String minecraftVersionName() { return mc(); }
        @Override public java.util.OptionalInt buildNumber() { return java.util.OptionalInt.empty(); }
        @Override public java.time.Instant buildTime() { return java.time.Instant.EPOCH; }
        @Override public java.util.Optional<String> gitBranch() { return java.util.Optional.empty(); }
        @Override public java.util.Optional<String> gitCommit() { return java.util.Optional.empty(); }

        @Override
        public String asString(StringRepresentation r) {
            return r == StringRepresentation.VERSION_SIMPLE ? mc() + "-kremenmc" : "KremenMC " + version() + " (Paper API " + mc() + ", Java plugin bridge)";
        }
    }

    /** Backs the deprecated JavaPlugin.getPluginLoader(). */
    @SuppressWarnings("removal")
    public static final class Loader implements org.bukkit.plugin.PluginLoader {
        public Loader() {}

        private static PluginManagerImpl pm() { return Bridge.get().pluginManager; }

        @Override public Plugin loadPlugin(java.io.File f) throws org.bukkit.plugin.InvalidPluginException {
            try {
                return pm().loadPlugin(f);
            } catch (org.bukkit.plugin.InvalidDescriptionException e) {
                throw new org.bukkit.plugin.InvalidPluginException(e);
            }
        }

        @Override public org.bukkit.plugin.PluginDescriptionFile getPluginDescription(java.io.File f) throws org.bukkit.plugin.InvalidDescriptionException { return PluginManagerImpl.describe(f); }
        @Override public java.util.regex.Pattern[] getPluginFileFilters() { return new java.util.regex.Pattern[] {java.util.regex.Pattern.compile("\\.jar$")}; }

        @Override
        public java.util.Map<Class<? extends org.bukkit.event.Event>, java.util.Set<org.bukkit.plugin.RegisteredListener>> createRegisteredListeners(org.bukkit.event.Listener l, Plugin p) {
            throw new UnsupportedOperationException("kremenmc bridge: use PluginManager.registerEvents");
        }

        @Override public void enablePlugin(Plugin p) { pm().enablePlugin(p); }
        @Override public void disablePlugin(Plugin p) { pm().disablePlugin(p); }
    }
}
