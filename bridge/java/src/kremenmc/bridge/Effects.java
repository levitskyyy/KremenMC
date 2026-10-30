package kremenmc.bridge;

import java.util.ArrayList;
import java.util.Collection;
import java.util.List;
import java.util.Map;
import java.util.UUID;
import org.bukkit.NamespacedKey;
import org.bukkit.Registry;
import org.bukkit.attribute.Attribute;
import org.bukkit.attribute.AttributeInstance;
import org.bukkit.attribute.AttributeModifier;
import org.bukkit.potion.PotionEffect;
import org.bukkit.potion.PotionEffectType;

/** Attributes and potion effects of Go-owned living entities, addressed by UUID. */
final class Effects {
    private Effects() {}

    static AttributeInstance attribute(Bridge b, UUID id, Attribute a) {
        if (a == null) return null;
        Map<String, Object> m = Wire.map(b.query("attribute", "uuid", id, "name", a.getKey().toString()));
        if (!Wire.bool(m, "has")) return null;
        return Stubs.proxy(AttributeInstance.class, new AttributeImpl(b, id, a));
    }

    public static final class AttributeImpl {
        final Bridge b;
        final UUID id;
        final Attribute attr;

        AttributeImpl(Bridge b, UUID id, Attribute attr) {
            this.b = b;
            this.id = id;
            this.attr = attr;
        }

        private Map<String, Object> state() { return Wire.map(b.query("attribute", "uuid", id, "name", attr.getKey().toString())); }

        public Attribute getAttribute() { return attr; }
        public double getBaseValue() { return Wire.dbl(state(), "base"); }
        public void setBaseValue(double v) { b.action("attribute_base", "uuid", id, "name", attr.getKey().toString(), "value", v); }
        public double getValue() { return Wire.dbl(state(), "value"); }
        public double getDefaultValue() { return Wire.dbl(state(), "default"); }

        public Collection<AttributeModifier> getModifiers() {
            List<AttributeModifier> out = new ArrayList<>();
            for (Object o : Wire.list(state().get("modifiers"))) {
                Map<String, Object> m = Wire.map(o);
                NamespacedKey k = NamespacedKey.fromString(Wire.str(m, "id"));
                if (k == null) continue;
                int op = (int) Wire.lng(m, "op");
                AttributeModifier.Operation[] ops = AttributeModifier.Operation.values();
                out.add(new AttributeModifier(k, Wire.dbl(m, "amount"), ops[Math.max(0, Math.min(op, ops.length - 1))]));
            }
            return out;
        }

        public AttributeModifier getModifier(net.kyori.adventure.key.Key key) {
            for (AttributeModifier m : getModifiers()) if (m.getKey().equals(key)) return m;
            return null;
        }

        public AttributeModifier getModifier(UUID uuid) {
            for (AttributeModifier m : getModifiers()) if (m.getUniqueId().equals(uuid)) return m;
            return null;
        }

        public void addModifier(AttributeModifier m) {
            b.action("attribute_mod", "uuid", id, "name", attr.getKey().toString(), "id", m.getKey().toString(),
                    "amount", m.getAmount(), "op", m.getOperation().ordinal());
        }

        public void addTransientModifier(AttributeModifier m) { addModifier(m); }
        public void removeModifier(AttributeModifier m) { removeModifier((net.kyori.adventure.key.Key) m.getKey()); }

        public void removeModifier(net.kyori.adventure.key.Key key) {
            b.action("attribute_mod", "uuid", id, "name", attr.getKey().toString(), "id", key.asString(), "remove", true);
        }

        public void removeModifier(UUID uuid) {
            AttributeModifier m = getModifier(uuid);
            if (m != null) removeModifier(m);
        }

        @Override public String toString() { return "CraftAttributeInstance{" + attr.getKey() + "}"; }
    }

    static PotionEffectType type(String key) {
        NamespacedKey k = NamespacedKey.fromString(key);
        return k == null ? null : Registry.MOB_EFFECT.get(k);
    }

    static List<PotionEffect> active(Bridge b, UUID id) {
        List<PotionEffect> out = new ArrayList<>();
        for (Object o : Wire.list(b.query("effects", "uuid", id))) {
            Map<String, Object> m = Wire.map(o);
            PotionEffectType t = type(Wire.str(m, "type"));
            if (t == null) continue;
            out.add(new PotionEffect(t, (int) Wire.lng(m, "duration"), (int) Wire.lng(m, "amplifier"),
                    Wire.bool(m, "ambient"), Wire.bool(m, "particles"), Wire.bool(m, "icon")));
        }
        return out;
    }

    static PotionEffect get(Bridge b, UUID id, PotionEffectType t) {
        if (t == null) return null;
        for (PotionEffect e : active(b, id)) if (e.getType().getKey().equals(t.getKey())) return e;
        return null;
    }

    static boolean add(Bridge b, UUID id, PotionEffect e) {
        if (e == null) return false;
        b.action("effect_add", "uuid", id, "type", e.getType().getKey().toString(), "amplifier", e.getAmplifier(),
                "duration", e.getDuration(), "ambient", e.isAmbient(), "particles", e.hasParticles(), "icon", e.hasIcon());
        return true;
    }

    static void remove(Bridge b, UUID id, PotionEffectType t) {
        if (t != null) b.action("effect_remove", "uuid", id, "type", t.getKey().toString());
    }

    static void clear(Bridge b, UUID id) { b.action("effect_remove", "uuid", id, "type", ""); }

    /** LivingEntity methods shared by players and mobs. */
    abstract static class Living extends Players.SenderImpl {
        Living(Bridge b) { super(b); }

        public AttributeInstance getAttribute(Attribute a) { return attribute(b, target(), a); }
        public boolean addPotionEffect(PotionEffect e) { return add(b, target(), e); }
        public boolean addPotionEffect(PotionEffect e, boolean force) { return add(b, target(), e); }

        public boolean addPotionEffects(Collection<PotionEffect> es) {
            for (PotionEffect e : es) add(b, target(), e);
            return true;
        }

        public void removePotionEffect(PotionEffectType t) { remove(b, target(), t); }
        public Collection<PotionEffect> getActivePotionEffects() { return active(b, target()); }
        public boolean hasPotionEffect(PotionEffectType t) { return get(b, target(), t) != null; }
        public PotionEffect getPotionEffect(PotionEffectType t) { return get(b, target(), t); }

        public boolean clearActivePotionEffects() {
            boolean any = !active(b, target()).isEmpty();
            clear(b, target());
            return any;
        }
    }
}
