package net.minecraft.core;

public interface RegistryAccess {
    interface Frozen extends RegistryAccess {}

    Frozen EMPTY = new Frozen() {};
}
