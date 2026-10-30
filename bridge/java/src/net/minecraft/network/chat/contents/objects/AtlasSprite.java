package net.minecraft.network.chat.contents.objects;

import net.minecraft.resources.Identifier;

public record AtlasSprite(Identifier atlas, Identifier sprite) implements ObjectInfo {}
