package net.minecraft.resources;

import java.util.Objects;
import net.minecraft.core.Registry;

public final class ResourceKey<T> {
    private static final Identifier ROOT = Identifier.withDefaultNamespace("root");

    private final Identifier registryName;
    private final Identifier identifier;

    private ResourceKey(Identifier registryName, Identifier identifier) {
        this.registryName = registryName;
        this.identifier = identifier;
    }

    public static <T> ResourceKey<Registry<T>> createRegistryKey(Identifier id) { return new ResourceKey<>(ROOT, id); }

    public static <T> ResourceKey<T> create(ResourceKey<? extends Registry<T>> registry, Identifier id) {
        return new ResourceKey<>(registry.identifier, id);
    }

    public Identifier registry() { return registryName; }
    public Identifier identifier() { return identifier; }

    @Override public boolean equals(Object o) { return o instanceof ResourceKey<?> k && k.registryName.equals(registryName) && k.identifier.equals(identifier); }
    @Override public int hashCode() { return Objects.hash(registryName, identifier); }
    @Override public String toString() { return "ResourceKey[" + registryName + " / " + identifier + "]"; }
}
