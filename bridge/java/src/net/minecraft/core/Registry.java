package net.minecraft.core;

import net.minecraft.resources.Identifier;

/** A registry seen from plugins. Registry contents live on the Go side, so lookups find empty registries. */
public interface Registry<T> {
    Registry<Registry<?>> ROOT = id -> Registry.EMPTY;
    Registry<?> EMPTY = id -> null;

    T getValue(Identifier id);
}
