package net.minecraft.network.chat;

import net.minecraft.resources.Identifier;

public interface FontDescription {
    FontDescription.Resource DEFAULT = new Resource(Identifier.withDefaultNamespace("default"));

    record Resource(Identifier id) implements FontDescription {}
}
