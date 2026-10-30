package kremenmc.bridge;

import com.destroystokyo.paper.profile.PlayerProfile;
import com.destroystokyo.paper.profile.ProfileProperty;
import java.lang.reflect.Field;
import java.util.ArrayList;
import java.util.Collection;
import java.util.EnumSet;
import java.util.HashMap;
import java.util.LinkedHashMap;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.Objects;
import java.util.Set;
import java.util.UUID;
import java.util.concurrent.ConcurrentHashMap;
import java.util.function.Consumer;
import net.kyori.adventure.text.Component;
import org.bukkit.Bukkit;
import org.bukkit.Color;
import org.bukkit.Material;
import org.bukkit.NamespacedKey;
import org.bukkit.OfflinePlayer;
import org.bukkit.Registry;
import org.bukkit.enchantments.Enchantment;
import org.bukkit.inventory.ItemFlag;
import org.bukkit.inventory.ItemStack;
import org.bukkit.inventory.ItemType;
import org.bukkit.inventory.meta.Damageable;
import org.bukkit.inventory.meta.ItemMeta;
import org.bukkit.inventory.meta.LeatherArmorMeta;
import org.bukkit.inventory.meta.Repairable;
import org.bukkit.inventory.meta.SkullMeta;
import org.bukkit.persistence.PersistentDataAdapterContext;
import org.bukkit.persistence.PersistentDataContainer;
import org.bukkit.persistence.PersistentDataType;

/**
 * Item stacks and their meta. A stack is an item id, a count and a data
 * component patch (wire layout in internal/bridge/inv.go); decoded components
 * remember their raw bytes so untouched ones go back to Go unchanged.
 */
final class Items {
    private Items() {}

    static final String CUSTOM_NAME = "minecraft:custom_name", ITEM_NAME = "minecraft:item_name", LORE = "minecraft:lore",
            ENCHANTS = "minecraft:enchantments", STORED = "minecraft:stored_enchantments", DAMAGE = "minecraft:damage",
            MAX_DAMAGE = "minecraft:max_damage", MAX_STACK = "minecraft:max_stack_size", REPAIR = "minecraft:repair_cost",
            UNBREAKABLE = "minecraft:unbreakable", GLINT = "minecraft:enchantment_glint_override", MODEL = "minecraft:item_model",
            TOOLTIP = "minecraft:tooltip_display", CMD = "minecraft:custom_model_data", DYED = "minecraft:dyed_color",
            PROFILE = "minecraft:profile", CUSTOM_DATA = "minecraft:custom_data", GLIDER = "minecraft:glider";

    static final String PDC_ROOT = "PublicBukkitValues";

    /** Component patch: name -> value (byte[] raw or a decoded value), plus removals. */
    static final class Data {
        final Map<String, Object> c = new LinkedHashMap<>();
        final Map<String, Object[]> orig = new HashMap<>();
        final Set<String> rm = new LinkedHashSet<>();

        Data copy() {
            Data d = new Data();
            c.forEach((k, v) -> d.c.put(k, copyValue(v)));
            d.orig.putAll(orig);
            d.rm.addAll(rm);
            return d;
        }

        Object get(String k) { return c.get(k); }

        void put(String k, Object v) {
            if (v == null) {
                c.remove(k);
            } else {
                c.put(k, v);
                rm.remove(k);
            }
        }

        Map<String, Object> wire() {
            Map<String, Object> out = new LinkedHashMap<>();
            for (Map.Entry<String, Object> e : c.entrySet()) {
                Object[] o = orig.get(e.getKey());
                out.put(e.getKey(), o != null && Objects.equals(o[0], e.getValue()) ? o[1] : e.getValue());
            }
            return out;
        }

        @Override public boolean equals(Object o) { return o instanceof Data d && deepEquals(c, d.c) && rm.equals(d.rm); }
        @Override public int hashCode() { return c.keySet().hashCode() * 31 + rm.hashCode(); }
    }

    @SuppressWarnings("unchecked")
    static Object copyValue(Object v) {
        return switch (v) {
            case byte[] b -> b.clone();
            case List<?> l -> { List<Object> c = new ArrayList<>(); for (Object o : l) c.add(copyValue(o)); yield c; }
            case Map<?, ?> m -> { Map<String, Object> c = new LinkedHashMap<>(); ((Map<String, Object>) m).forEach((k, x) -> c.put(k, copyValue(x))); yield c; }
            default -> v;
        };
    }

    static boolean deepEquals(Map<String, Object> a, Map<String, Object> b) {
        if (!a.keySet().equals(b.keySet())) return false;
        for (Map.Entry<String, Object> e : a.entrySet()) {
            Object x = e.getValue(), y = b.get(e.getKey());
            if (x instanceof byte[] xb && y instanceof byte[] yb) {
                if (!java.util.Arrays.equals(xb, yb)) return false;
            } else if (!Objects.equals(x, y)) {
                return false;
            }
        }
        return true;
    }

    // ---- stacks ----

    /** The server's item stack, the delegate of every Bukkit ItemStack. */
    public static final class Stack extends ItemStack {
        String id;
        int amount;
        Data data;

        Stack(String id, int amount, Data data) {
            super();
            this.id = id;
            this.amount = amount;
            this.data = data;
        }

        @Override public Material getType() { return material(id); }
        @Override public void setType(Material m) { id = m.getKey().toString(); }
        @Override public ItemStack withType(Material m) { Stack s = copy(); s.setType(m); return s; }
        @Override public int getAmount() { return amount; }
        @Override public void setAmount(int n) { amount = n; }
        @Override public boolean isEmpty() { return amount <= 0 || id.equals("minecraft:air"); }
        @Override public int getMaxStackSize() {
            return data.get(MAX_STACK) instanceof Number n ? n.intValue() : info(id).maxStack;
        }
        @Override public short getDurability() { return data.get(DAMAGE) instanceof Number n ? n.shortValue() : 0; }
        @Override public void setDurability(short d) { data.put(DAMAGE, d == 0 ? null : (long) d); }
        @Override public ItemMeta getItemMeta() { return isEmpty() ? null : meta(id, data.copy()); }
        @Override public boolean hasItemMeta() { return !isEmpty() && !data.c.isEmpty(); }
        @Override public boolean setItemMeta(ItemMeta m) {
            if (m == null) {
                data = new Data();
                return true;
            }
            if (!(Stubs.implOf(m) instanceof MetaImpl mi)) return false;
            data = mi.d.copy();
            return true;
        }
        @Override public Map<Enchantment, Integer> getEnchantments() { return enchants(data, ENCHANTS); }
        @Override public boolean containsEnchantment(Enchantment e) { return getEnchantments().containsKey(e); }
        @Override public int getEnchantmentLevel(Enchantment e) { return getEnchantments().getOrDefault(e, 0); }
        @Override public void addUnsafeEnchantment(Enchantment e, int level) { setEnchant(data, ENCHANTS, e, level); }
        @Override public int removeEnchantment(Enchantment e) {
            int old = getEnchantmentLevel(e);
            setEnchant(data, ENCHANTS, e, 0);
            return old;
        }
        @Override public void removeEnchantments() { data.put(ENCHANTS, null); }
        @Override public boolean isSimilar(ItemStack o) {
            if (o == null) return false;
            if (o == this) return true;
            Stack s = unwrap(o);
            return s != null && s.id.equals(id) && s.data.equals(data);
        }
        @Override public boolean equals(Object o) {
            return o instanceof ItemStack s && isSimilar(s) && s.getAmount() == amount;
        }
        @Override public int hashCode() { return (id.hashCode() * 31 + amount) * 31 + data.hashCode(); }
        @Override public Stack clone() { return copy(); }
        Stack copy() { return new Stack(id, amount, data.copy()); }
        @Override public String toString() { return "ItemStack{" + id + " x " + amount + (data.c.isEmpty() ? "" : ", " + data.c.keySet()) + "}"; }
        @Override public String translationKey() { return (getType().isBlock() ? "block." : "item.") + id.replace(':', '.'); }
        @Override public Component effectiveName() {
            if (data.get(CUSTOM_NAME) instanceof String s) return Text.component(s);
            if (data.get(ITEM_NAME) instanceof String s) return Text.component(s);
            return Component.translatable(translationKey());
        }
        @Override public io.papermc.paper.persistence.PersistentDataContainerView getPersistentDataContainer() { return new Pdc(data, true); }
        @Override public boolean editPersistentDataContainer(Consumer<PersistentDataContainer> f) {
            f.accept(new Pdc(data, false));
            return true;
        }
        @Override public Map<String, Object> serialize() {
            Map<String, Object> m = new LinkedHashMap<>();
            m.put("id", id);
            m.put("count", amount);
            return m;
        }
        @Override public byte[] serializeAsBytes() {
            return Wire.encode((byte) 0, 0, 0, wire(this));
        }
    }

    static Material material(String id) {
        NamespacedKey k = NamespacedKey.fromString(id);
        Material m = k == null ? null : Material.getMaterial(k.getKey().toUpperCase(Locale.ROOT));
        return m == null ? Material.AIR : m;
    }

    private static Field delegateField;

    /** The server stack behind any Bukkit ItemStack, or null for an empty one. */
    static Stack unwrap(ItemStack s) {
        if (s == null) return null;
        if (s instanceof Stack k) return k;
        try {
            Field f = delegateField;
            if (f == null) {
                f = ItemStack.class.getDeclaredField("craftDelegate");
                f.setAccessible(true);
                delegateField = f;
            }
            if (f.get(s) instanceof Stack k) return k;
        } catch (ReflectiveOperationException ignored) {
        }
        Stack k = new Stack(s.getType().getKey().toString(), s.getAmount(), new Data());
        if (s.hasItemMeta()) k.setItemMeta(s.getItemMeta());
        return k;
    }

    static ItemStack empty() { return new Stack("minecraft:air", 0, new Data()); }

    static Object wire(ItemStack s) {
        Stack k = unwrap(s);
        if (k == null || k.isEmpty()) return null;
        Map<String, Object> m = new LinkedHashMap<>();
        m.put("id", k.id);
        m.put("count", k.amount);
        m.put("c", k.data.wire());
        if (!k.data.rm.isEmpty()) m.put("rm", new ArrayList<>(k.data.rm));
        return m;
    }

    static List<Object> wire(ItemStack[] items) {
        List<Object> out = new ArrayList<>(items.length);
        for (ItemStack s : items) out.add(wire(s));
        return out;
    }

    /** A stack from the wire; air for nil. */
    static ItemStack fromWire(Object o) {
        Map<String, Object> m = Wire.map(o);
        if (m.isEmpty()) return empty();
        Data d = new Data();
        Map<String, Object> raw = Wire.map(m.get("o"));
        for (Map.Entry<String, Object> e : Wire.map(m.get("c")).entrySet()) {
            d.c.put(e.getKey(), e.getValue());
            if (raw.get(e.getKey()) instanceof byte[] b) d.orig.put(e.getKey(), new Object[] {copyValue(e.getValue()), b});
        }
        for (Object r : Wire.list(m.get("rm"))) if (r instanceof String s) d.rm.add(s);
        return new Stack(Wire.str(m, "id"), (int) Wire.lng(m, "count"), d);
    }

    /** Like fromWire, but null for an empty slot (Bukkit getContents style). */
    static ItemStack fromWireOrNull(Object o) {
        return o == null ? null : fromWire(o);
    }

    // ---- item info ----

    record Info(int maxStack, int maxDamage, boolean exists) {}

    private static final Map<String, Info> INFO = new ConcurrentHashMap<>();
    static final int OP_ITEM_INFO = 71;

    static Info info(String id) {
        return INFO.computeIfAbsent(id, k -> {
            Bridge b = Bridge.get();
            if (b == null) return new Info(64, 0, true);
            Map<String, Object> m = Wire.map(b.call(OP_ITEM_INFO, k).get(0));
            return new Info((int) Math.max(1, Wire.lng(m, "maxStack")), (int) Wire.lng(m, "maxDamage"), Wire.bool(m, "exists"));
        });
    }

    // ---- enchantments ----

    static Map<Enchantment, Integer> enchants(Data d, String comp) {
        Map<Enchantment, Integer> out = new LinkedHashMap<>();
        for (Map.Entry<String, Object> e : Wire.map(d.get(comp)).entrySet()) {
            NamespacedKey k = NamespacedKey.fromString(e.getKey());
            Enchantment en = k == null ? null : Registry.ENCHANTMENT.get(k);
            if (en != null && e.getValue() instanceof Number n) out.put(en, n.intValue());
        }
        return out;
    }

    static void setEnchant(Data d, String comp, Enchantment e, int level) {
        Map<String, Object> m = new LinkedHashMap<>(Wire.map(d.get(comp)));
        if (level <= 0) m.remove(e.getKey().toString());
        else m.put(e.getKey().toString(), (long) level);
        d.put(comp, m.isEmpty() ? null : m);
    }

    // ---- meta ----

    interface Meta extends Damageable, Repairable { @Override Meta clone(); }
    interface Skull extends Meta, SkullMeta { @Override Skull clone(); }
    interface Leather extends Meta, LeatherArmorMeta { @Override Leather clone(); }

    static ItemMeta meta(String id, Data d) {
        MetaImpl impl = new MetaImpl(id, d);
        return Stubs.proxy(metaClass(id), impl);
    }

    static Class<? extends Meta> metaClass(String id) {
        if (id.endsWith("player_head") || id.endsWith("_skull") || id.endsWith("_head")) return Skull.class;
        if (id.startsWith("minecraft:leather_") || id.equals("minecraft:wolf_armor")) return Leather.class;
        return Meta.class;
    }

    private static final Map<ItemFlag, List<String>> FLAGS = new EnumMap();

    @SuppressWarnings("serial")
    private static final class EnumMap extends java.util.EnumMap<ItemFlag, List<String>> {
        EnumMap() {
            super(ItemFlag.class);
            put(ItemFlag.HIDE_ENCHANTS, List.of(ENCHANTS));
            put(ItemFlag.HIDE_ATTRIBUTES, List.of("minecraft:attribute_modifiers"));
            put(ItemFlag.HIDE_UNBREAKABLE, List.of(UNBREAKABLE));
            put(ItemFlag.HIDE_DESTROYS, List.of("minecraft:can_break"));
            put(ItemFlag.HIDE_PLACED_ON, List.of("minecraft:can_place_on"));
            put(ItemFlag.HIDE_DYE, List.of(DYED));
            put(ItemFlag.HIDE_ARMOR_TRIM, List.of("minecraft:trim"));
            put(ItemFlag.HIDE_STORED_ENCHANTS, List.of(STORED));
            put(ItemFlag.HIDE_ADDITIONAL_TOOLTIP, List.of("minecraft:banner_patterns", "minecraft:bees", "minecraft:block_entity_data",
                    "minecraft:block_state", "minecraft:bundle_contents", "minecraft:charged_projectiles", "minecraft:container",
                    "minecraft:container_loot", "minecraft:firework_explosion", "minecraft:fireworks", "minecraft:instrument",
                    "minecraft:map_id", "minecraft:painting/variant", "minecraft:pot_decorations", "minecraft:potion_contents",
                    "minecraft:tropical_fish/pattern", "minecraft:written_book_content", "minecraft:jukebox_playable",
                    "minecraft:ominous_bottle_amplifier", "minecraft:suspicious_stew_effects", "minecraft:note_block_sound"));
        }
    }

    public static final class MetaImpl {
        final String id;
        final Data d;

        MetaImpl(String id, Data d) {
            this.id = id;
            this.d = d;
        }

        private String str(String k) { return d.get(k) instanceof String s ? s : null; }
        private Map<String, Object> map(String k) { return new LinkedHashMap<>(Wire.map(d.get(k))); }

        public boolean hasCustomName() { return str(CUSTOM_NAME) != null; }
        public Component customName() { return hasCustomName() ? Text.component(str(CUSTOM_NAME)) : null; }
        public void customName(Component c) { d.put(CUSTOM_NAME, c == null ? null : Text.json(c)); }
        public boolean hasDisplayName() { return hasCustomName(); }
        public Component displayName() { return customName(); }
        public void displayName(Component c) { customName(c); }
        public String getDisplayName() { return hasCustomName() ? Text.legacy(customName()) : ""; }
        public void setDisplayName(String s) { d.put(CUSTOM_NAME, s == null ? null : Text.jsonLegacy(s)); }
        public boolean hasItemName() { return str(ITEM_NAME) != null; }
        public Component itemName() { return hasItemName() ? Text.component(str(ITEM_NAME)) : Component.translatable("item." + id.replace(':', '.')); }
        public void itemName(Component c) { d.put(ITEM_NAME, c == null ? null : Text.json(c)); }
        public String getItemName() { return hasItemName() ? Text.legacy(itemName()) : ""; }
        public void setItemName(String s) { d.put(ITEM_NAME, s == null ? null : Text.jsonLegacy(s)); }
        public boolean hasLocalizedName() { return false; }
        public String getLocalizedName() { return ""; }
        public void setLocalizedName(String s) {}

        public boolean hasLore() { return !Wire.list(d.get(LORE)).isEmpty(); }
        public List<Component> lore() {
            if (!hasLore()) return null;
            List<Component> out = new ArrayList<>();
            for (Object o : Wire.list(d.get(LORE))) out.add(Text.component((String) o));
            return out;
        }
        public void lore(List<? extends Component> l) {
            if (l == null || l.isEmpty()) {
                d.put(LORE, null);
                return;
            }
            List<Object> out = new ArrayList<>();
            for (Component c : l) out.add(Text.json(c == null ? Component.empty() : c));
            d.put(LORE, out);
        }
        public List<String> getLore() {
            List<Component> l = lore();
            if (l == null) return null;
            List<String> out = new ArrayList<>();
            for (Component c : l) out.add(Text.legacy(c));
            return out;
        }
        public void setLore(List<String> l) {
            if (l == null || l.isEmpty()) {
                d.put(LORE, null);
                return;
            }
            List<Object> out = new ArrayList<>();
            for (String s : l) out.add(Text.jsonLegacy(s == null ? "" : s));
            d.put(LORE, out);
        }

        public boolean hasCustomModelData() { return !Wire.list(map(CMD).get("floats")).isEmpty(); }
        public int getCustomModelData() {
            List<Object> f = Wire.list(map(CMD).get("floats"));
            return f.isEmpty() ? 0 : ((Number) f.getFirst()).intValue();
        }
        public void setCustomModelData(Integer v) {
            if (v == null) {
                d.put(CMD, null);
                return;
            }
            Map<String, Object> m = new LinkedHashMap<>();
            m.put("floats", List.of((double) v.floatValue()));
            m.put("flags", List.of());
            m.put("strings", List.of());
            m.put("colors", List.of());
            d.put(CMD, m);
        }
        public boolean hasCustomModelDataComponent() { return d.get(CMD) != null; }

        public boolean hasEnchants() { return !enchants(d, ENCHANTS).isEmpty(); }
        public boolean hasEnchant(Enchantment e) { return enchants(d, ENCHANTS).containsKey(e); }
        public int getEnchantLevel(Enchantment e) { return enchants(d, ENCHANTS).getOrDefault(e, 0); }
        public Map<Enchantment, Integer> getEnchants() { return enchants(d, ENCHANTS); }
        public boolean addEnchant(Enchantment e, int level, boolean ignoreRestrictions) {
            if (!ignoreRestrictions && level > e.getMaxLevel()) return false;
            if (getEnchantLevel(e) == level) return false;
            setEnchant(d, ENCHANTS, e, level);
            return true;
        }
        public boolean removeEnchant(Enchantment e) {
            boolean had = hasEnchant(e);
            setEnchant(d, ENCHANTS, e, 0);
            return had;
        }
        public void removeEnchantments() { d.put(ENCHANTS, null); }
        public boolean hasConflictingEnchant(Enchantment e) {
            for (Enchantment o : getEnchants().keySet()) if (!o.equals(e) && o.conflictsWith(e)) return true;
            return false;
        }
        public boolean hasStoredEnchants() { return !enchants(d, STORED).isEmpty(); }
        public Map<Enchantment, Integer> getStoredEnchants() { return enchants(d, STORED); }
        public boolean addStoredEnchant(Enchantment e, int level, boolean ignore) { setEnchant(d, STORED, e, level); return true; }

        private Set<String> hidden() {
            Set<String> out = new LinkedHashSet<>();
            for (Object o : Wire.list(map(TOOLTIP).get("hidden"))) if (o instanceof String s) out.add(s);
            return out;
        }
        private void tooltip(boolean hide, Set<String> hidden) {
            if (!hide && hidden.isEmpty()) {
                d.put(TOOLTIP, null);
                return;
            }
            Map<String, Object> m = new LinkedHashMap<>();
            m.put("hide", hide);
            m.put("hidden", new ArrayList<>(hidden));
            d.put(TOOLTIP, m);
        }
        public void addItemFlags(ItemFlag... fs) {
            Set<String> h = hidden();
            for (ItemFlag f : fs) h.addAll(FLAGS.getOrDefault(f, List.of()));
            tooltip(isHideTooltip(), h);
        }
        public void removeItemFlags(ItemFlag... fs) {
            Set<String> h = hidden();
            for (ItemFlag f : fs) FLAGS.getOrDefault(f, List.of()).forEach(h::remove);
            tooltip(isHideTooltip(), h);
        }
        public Set<ItemFlag> getItemFlags() {
            Set<String> h = hidden();
            Set<ItemFlag> out = EnumSet.noneOf(ItemFlag.class);
            for (Map.Entry<ItemFlag, List<String>> e : FLAGS.entrySet()) if (h.contains(e.getValue().getFirst())) out.add(e.getKey());
            return out;
        }
        public boolean hasItemFlag(ItemFlag f) { return getItemFlags().contains(f); }
        public boolean isHideTooltip() { return Wire.bool(map(TOOLTIP), "hide"); }
        public void setHideTooltip(boolean v) { tooltip(v, hidden()); }

        public boolean hasItemModel() { return str(MODEL) != null; }
        public NamespacedKey getItemModel() { return hasItemModel() ? NamespacedKey.fromString(str(MODEL)) : null; }
        public void setItemModel(NamespacedKey k) { d.put(MODEL, k == null ? null : k.toString()); }

        public boolean isUnbreakable() { return d.get(UNBREAKABLE) != null; }
        public void setUnbreakable(boolean v) { d.put(UNBREAKABLE, v ? Boolean.TRUE : null); }
        public boolean isGlider() { return d.get(GLIDER) != null; }
        public void setGlider(boolean v) { d.put(GLIDER, v ? Boolean.TRUE : null); }
        public boolean hasEnchantmentGlintOverride() { return d.get(GLINT) != null; }
        public Boolean getEnchantmentGlintOverride() { return d.get(GLINT) instanceof Boolean b ? b : null; }
        public void setEnchantmentGlintOverride(Boolean v) { d.put(GLINT, v); }

        public boolean hasMaxStackSize() { return d.get(MAX_STACK) != null; }
        public int getMaxStackSize() { return d.get(MAX_STACK) instanceof Number n ? n.intValue() : info(id).maxStack; }
        public void setMaxStackSize(Integer v) { d.put(MAX_STACK, v == null ? null : (long) (int) v); }

        public boolean hasDamage() { return getDamage() > 0; }
        public boolean hasDamageValue() { return d.get(DAMAGE) != null; }
        public int getDamage() { return d.get(DAMAGE) instanceof Number n ? n.intValue() : 0; }
        public void setDamage(int v) { d.put(DAMAGE, v <= 0 ? null : (long) v); }
        public void resetDamage() { d.put(DAMAGE, null); }
        public boolean hasMaxDamage() { return d.get(MAX_DAMAGE) != null || info(id).maxDamage > 0; }
        public int getMaxDamage() { return d.get(MAX_DAMAGE) instanceof Number n ? n.intValue() : info(id).maxDamage; }
        public void setMaxDamage(Integer v) { d.put(MAX_DAMAGE, v == null ? null : (long) (int) v); }
        public boolean hasRepairCost() { return d.get(REPAIR) != null; }
        public int getRepairCost() { return d.get(REPAIR) instanceof Number n ? n.intValue() : 0; }
        public void setRepairCost(int v) { d.put(REPAIR, v == 0 ? null : (long) v); }

        public PersistentDataContainer getPersistentDataContainer() { return new Pdc(d, false); }

        // leather
        public Color getColor() { return d.get(DYED) instanceof Number n ? Color.fromRGB(n.intValue() & 0xFFFFFF) : Color.fromRGB(0xA06540); }
        public void setColor(Color c) { d.put(DYED, c == null ? null : (long) c.asRGB()); }
        public boolean isDyed() { return d.get(DYED) != null; }

        // skull
        public boolean hasOwner() { return d.get(PROFILE) != null; }
        public String getOwner() { return hasOwner() ? Wire.str(map(PROFILE), "name") : null; }
        public boolean setOwner(String name) {
            if (name == null) {
                d.put(PROFILE, null);
                return true;
            }
            d.put(PROFILE, profileMap(null, name, List.of()));
            return true;
        }
        public OfflinePlayer getOwningPlayer() {
            Map<String, Object> m = map(PROFILE);
            if (m.get("uuid") instanceof UUID u) return Bukkit.getOfflinePlayer(u);
            String n = Wire.str(m, "name");
            return n.isEmpty() ? null : Bukkit.getOfflinePlayer(n);
        }
        public boolean setOwningPlayer(OfflinePlayer p) {
            if (p == null) {
                d.put(PROFILE, null);
                return true;
            }
            d.put(PROFILE, profileMap(p.getUniqueId(), p.getName(), List.of()));
            return true;
        }
        public PlayerProfile getPlayerProfile() {
            if (!hasOwner()) return null;
            Map<String, Object> m = map(PROFILE);
            PlayerProfile p = Players.profile(m.get("uuid") instanceof UUID u ? u : null, Wire.str(m, "name").isEmpty() ? null : Wire.str(m, "name"));
            for (Object o : Wire.list(m.get("properties"))) {
                Map<String, Object> pm = Wire.map(o);
                String sig = Wire.str(pm, "signature");
                p.setProperty(new ProfileProperty(Wire.str(pm, "name"), Wire.str(pm, "value"), sig.isEmpty() ? null : sig));
            }
            return p;
        }
        public void setPlayerProfile(PlayerProfile p) {
            if (p == null) {
                d.put(PROFILE, null);
                return;
            }
            d.put(PROFILE, profileMap(p.getId(), p.getName(), p.getProperties()));
        }

        public Meta clone() { return (Meta) meta(id, d.copy()); }
        public Map<String, Object> serialize() { return new LinkedHashMap<>(d.wire()); }
        public String getAsString() { return d.c.keySet().toString(); }
        public String getAsComponentString() { return getAsString(); }

        @Override public boolean equals(Object o) { return o instanceof MetaImpl m && m.d.equals(d); }
        @Override public int hashCode() { return d.hashCode(); }
        @Override public String toString() { return "ItemMeta{" + id + " " + d.c.keySet() + "}"; }
    }

    static Map<String, Object> profileMap(UUID id, String name, Collection<ProfileProperty> props) {
        Map<String, Object> m = new LinkedHashMap<>();
        if (name != null) m.put("name", name);
        if (id != null) m.put("uuid", id);
        List<Object> ps = new ArrayList<>();
        for (ProfileProperty p : props) {
            Map<String, Object> pm = new LinkedHashMap<>();
            pm.put("name", p.getName());
            pm.put("value", p.getValue());
            if (p.getSignature() != null) pm.put("signature", p.getSignature());
            ps.add(pm);
        }
        m.put("properties", ps);
        return m;
    }

    // ---- persistent data ----

    /** PersistentDataContainer over custom_data.PublicBukkitValues (or a nested compound). */
    static final class Pdc implements PersistentDataContainer {
        private final Data owner;
        private final Map<String, Object> nested;
        private final boolean readOnly;

        Pdc(Data owner, boolean readOnly) {
            this.owner = owner;
            this.nested = null;
            this.readOnly = readOnly;
        }

        Pdc(Map<String, Object> nested) {
            this.owner = null;
            this.nested = nested;
            this.readOnly = false;
        }

        @SuppressWarnings("unchecked")
        private Map<String, Object> values() {
            if (nested != null) return nested;
            Map<String, Object> root = Nbt.read(owner.get(CUSTOM_DATA) instanceof byte[] b ? b : null);
            return root.get(PDC_ROOT) instanceof Map<?, ?> m ? (Map<String, Object>) m : new LinkedHashMap<>();
        }

        private void store(Map<String, Object> v) {
            if (nested != null) return;
            if (readOnly) throw new UnsupportedOperationException("read-only persistent data view");
            Map<String, Object> root = Nbt.read(owner.get(CUSTOM_DATA) instanceof byte[] b ? b : null);
            if (v.isEmpty()) root.remove(PDC_ROOT);
            else root.put(PDC_ROOT, v);
            owner.put(CUSTOM_DATA, root.isEmpty() ? null : Nbt.write(root));
        }

        @Override public <P, C> void set(NamespacedKey key, PersistentDataType<P, C> type, C value) {
            Map<String, Object> v = values();
            v.put(key.toString(), toTag(type.toPrimitive(value, CONTEXT)));
            store(v);
        }

        @Override public void remove(NamespacedKey key) {
            Map<String, Object> v = values();
            if (v.remove(key.toString()) != null) store(v);
        }

        @Override public <P, C> boolean has(NamespacedKey key, PersistentDataType<P, C> type) {
            Object t = values().get(key.toString());
            return t != null && fromTag(t, type.getPrimitiveType()) != null;
        }

        @Override public boolean has(NamespacedKey key) { return values().containsKey(key.toString()); }

        @Override public <P, C> C get(NamespacedKey key, PersistentDataType<P, C> type) {
            Object t = values().get(key.toString());
            if (t == null) return null;
            P p = fromTag(t, type.getPrimitiveType());
            return p == null ? null : type.fromPrimitive(p, CONTEXT);
        }

        @Override public <P, C> C getOrDefault(NamespacedKey key, PersistentDataType<P, C> type, C def) {
            C v = get(key, type);
            return v == null ? def : v;
        }

        @Override public Set<NamespacedKey> getKeys() {
            Set<NamespacedKey> out = new LinkedHashSet<>();
            for (String k : values().keySet()) {
                NamespacedKey n = NamespacedKey.fromString(k);
                if (n != null) out.add(n);
            }
            return out;
        }

        @Override public boolean isEmpty() { return values().isEmpty(); }
        @Override public int getSize() { return values().size(); }
        @Override public PersistentDataAdapterContext getAdapterContext() { return CONTEXT; }

        @Override public void copyTo(PersistentDataContainer other, boolean replace) {
            if (!(other instanceof Pdc o)) return;
            Map<String, Object> mine = values(), theirs = o.values();
            for (Map.Entry<String, Object> e : mine.entrySet()) {
                if (replace || !theirs.containsKey(e.getKey())) theirs.put(e.getKey(), Nbt.deepCopy(e.getValue()));
            }
            o.store(theirs);
        }

        @Override public byte[] serializeToBytes() { return Nbt.write(values()); }

        @Override public void readFromBytes(byte[] bytes, boolean clear) {
            Map<String, Object> v = clear ? new LinkedHashMap<>() : values();
            v.putAll(Nbt.read(bytes));
            store(v);
        }

        @Override public PersistentDataType<?, ?> getPrimitiveStorageType(net.kyori.adventure.key.Key key) {
            Object t = values().get(key.asString());
            return switch (t) {
                case null -> null;
                case Byte x -> PersistentDataType.BYTE;
                case Short x -> PersistentDataType.SHORT;
                case Integer x -> PersistentDataType.INTEGER;
                case Long x -> PersistentDataType.LONG;
                case Float x -> PersistentDataType.FLOAT;
                case Double x -> PersistentDataType.DOUBLE;
                case String x -> PersistentDataType.STRING;
                case byte[] x -> PersistentDataType.BYTE_ARRAY;
                case int[] x -> PersistentDataType.INTEGER_ARRAY;
                case long[] x -> PersistentDataType.LONG_ARRAY;
                case Map<?, ?> x -> PersistentDataType.TAG_CONTAINER;
                default -> null;
            };
        }

        @Override public boolean equals(Object o) { return o instanceof Pdc p && Nbt.write(values()).length == Nbt.write(p.values()).length && java.util.Arrays.equals(Nbt.write(values()), Nbt.write(p.values())); }
        @Override public int hashCode() { return values().keySet().hashCode(); }
    }

    static final PersistentDataAdapterContext CONTEXT = () -> new Pdc(new LinkedHashMap<>());

    private static Object toTag(Object p) {
        return switch (p) {
            case Boolean b -> (byte) (b ? 1 : 0);
            case Pdc c -> new LinkedHashMap<>(c.values());
            case PersistentDataContainer[] cs -> {
                Nbt.TagList l = new Nbt.TagList(Nbt.COMPOUND);
                for (PersistentDataContainer c : cs) if (c instanceof Pdc pc) l.add(new LinkedHashMap<>(pc.values()));
                yield l;
            }
            case List<?> l -> {
                Nbt.TagList out = new Nbt.TagList(Nbt.END);
                for (Object o : l) out.add(toTag(o));
                if (!out.isEmpty()) out.type = Nbt.tagOf(out.getFirst());
                yield out;
            }
            default -> p;
        };
    }

    @SuppressWarnings("unchecked")
    private static <P> P fromTag(Object t, Class<P> type) {
        if (type.isInstance(t)) return (P) t;
        if (type == Boolean.class && t instanceof Byte b) return (P) Boolean.valueOf(b != 0);
        if (type == PersistentDataContainer.class && t instanceof Map<?, ?> m) return (P) new Pdc((Map<String, Object>) m);
        if (type == PersistentDataContainer[].class && t instanceof List<?> l) {
            PersistentDataContainer[] out = new PersistentDataContainer[l.size()];
            for (int i = 0; i < out.length; i++) out[i] = new Pdc(Wire.map(l.get(i)));
            return (P) out;
        }
        if (List.class.isAssignableFrom(type) && t instanceof List<?> l) return (P) new ArrayList<>(l);
        return null;
    }

    // ---- item types and factory ----

    /** Registry element for ItemType. */
    public static final class TypeImpl {
        final NamespacedKey key;
        final int ordinal;

        TypeImpl(NamespacedKey key, int ordinal) {
            this.key = key;
            this.ordinal = ordinal;
        }

        public NamespacedKey getKey() { return key; }
        public NamespacedKey getKeyOrThrow() { return key; }
        public NamespacedKey getKeyOrNull() { return key; }
        public net.kyori.adventure.key.Key key() { return key; }
        public boolean isRegistered() { return true; }
        public String translationKey() { return "item." + key.getNamespace() + "." + key.getKey(); }
        public String getTranslationKey() { return translationKey(); }
        public ItemStack createItemStack() { return createItemStack(1); }
        public ItemStack createItemStack(int n) { return new Stack(key.toString(), n, new Data()); }
        public int getMaxStackSize() { return info(key.toString()).maxStack; }
        public short getMaxDurability() { return (short) info(key.toString()).maxDamage; }
        public boolean hasBlockType() { return asMaterial().isBlock(); }
        public Material asMaterial() { return material(key.toString()); }
        public boolean isAir() { return key.getKey().equals("air"); }
        public Class<? extends ItemMeta> getItemMetaClass() { return metaClass(key.toString()); }
        public int ordinal() { return ordinal; }
        public String name() { return key.getKey().toUpperCase(Locale.ROOT); }
        @Override public boolean equals(Object o) { return o instanceof TypeImpl t && t.key.equals(key); }
        @Override public int hashCode() { return key.hashCode(); }
        @Override public String toString() { return key.toString(); }
    }

    public static final class FactoryImpl {
        public ItemMeta getItemMeta(Material m) { return m == null || m.isAir() ? null : meta(m.getKey().toString(), new Data()); }
        public boolean isApplicable(ItemMeta meta, ItemStack s) { return true; }
        public boolean isApplicable(ItemMeta meta, Material m) { return true; }
        public boolean equals(ItemMeta a, ItemMeta b) {
            if (a == null || b == null) return a == b || (a == null ? b : a) instanceof ItemMeta m && Stubs.implOf(m) instanceof MetaImpl mi && mi.d.c.isEmpty();
            return a.equals(b);
        }
        public ItemMeta asMetaFor(ItemMeta meta, ItemStack s) { return asMetaFor(meta, s.getType()); }
        public ItemMeta asMetaFor(ItemMeta meta, Material m) {
            return Stubs.implOf(meta) instanceof MetaImpl mi ? meta(m.getKey().toString(), mi.d.copy()) : meta;
        }
        public Color getDefaultLeatherColor() { return Color.fromRGB(0xA06540); }
        public ItemStack ensureServerConversions(ItemStack s) { return s; }
        public Component displayName(ItemStack s) { return unwrap(s).effectiveName(); }
        public String getI18NDisplayName(ItemStack s) { return Text.plain(displayName(s)); }
        public ItemStack createItemStack(String input) {
            String id = input.contains("[") ? input.substring(0, input.indexOf('[')) : input;
            NamespacedKey k = NamespacedKey.fromString(id.trim().toLowerCase(Locale.ROOT));
            if (k == null) throw new IllegalArgumentException("bad item " + input);
            return new Stack(k.toString(), 1, new Data());
        }
    }
}
