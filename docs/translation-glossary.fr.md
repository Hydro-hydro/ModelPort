# Glossaire Français (French Glossary)

Ce document présente les principaux termes encore utilisés par l'édition personnelle afin d'assurer la cohérence des traductions de l'agrégation de modèles, du routage, de la facturation et de l'interface administrateur.

## Concepts de base (Core Concepts)

| Chinois | Français | Anglais | Description |
|---------|----------|---------|-------------|
| 倍率 | Ratio | Ratio | Multiplicateur utilisé pour calculer les prix des modèles et des tâches |
| 令牌 | Jeton | Token | Identifiant d'accès API ou unité de texte traitée par un modèle |
| 渠道 | Canal | Channel | Canal d'accès à un fournisseur d'API en amont |
| 路由分组 | Groupe de routage | Route Group | Définit le routage, les modèles disponibles et le ratio de facturation |
| 额度 | Quota | Quota | Quota de service du portefeuille administrateur ou d'une clé API |

## Modèles et prix (Models and Pricing)

| Chinois | Français | Anglais | Description |
|---------|----------|---------|-------------|
| 提示 | Invite | Prompt | Contenu d'entrée du modèle |
| 补全 | Complétion | Completion | Contenu de sortie du modèle |
| 模型倍率 | Ratio du modèle | Model Ratio | Ratio de facturation d'un modèle |
| 固定价格 | Prix par appel | Price per call | Prix fixe par appel |
| 按量计费 | Paiement à l'utilisation | Pay-as-you-go | Facturation selon l'utilisation réelle |

## Compte administrateur (Administrator Account)

| Chinois | Français | Anglais | Description |
|---------|----------|---------|-------------|
| root 管理员 | Administrateur root | Root Administrator | Unique propriétaire du tableau de bord personnel |
| 管理员 Session | Session administrateur | Administrator Session | Session de connexion au tableau de bord |
| API Token | Jeton API | API Token | Identifiant pour les appels Relay et les tâches |
| 安全证明 | Preuve de sécurité | Security Proof | Preuve temporaire liée à la Session pour les opérations sensibles |

## Gestion des canaux (Channel Management)

| Chinois | Français | Anglais | Description |
|---------|----------|---------|-------------|
| 密钥 | Clé | Key | Clé API ou identifiant du canal en amont |
| 优先级 | Priorité | Priority | Priorité de sélection du canal |
| 权重 | Poids | Weight | Poids d'équilibrage de charge |
| 代理 | Proxy | Proxy | Adresse du serveur proxy |
| 模型映射 | Mappage de modèle | Model Mapping | Remplacement du nom du modèle par celui de l'amont |
| 厂商 | Fournisseur | Vendor | Fournisseur du modèle ou du service API |

## Facturation (Billing)

| Chinois | Français | Anglais | Description |
|---------|----------|---------|-------------|
| 预扣 | Pré-consommation | Pre-consumption | Déduction temporaire du quota estimé avant l'appel |
| 结算 | Règlement | Settlement | Ajustement selon l'utilisation réelle |
| 退款 | Remboursement | Refund | Retour du quota non utilisé après une erreur |
| 自动分组 | Groupes automatiques | Auto Groups | Essai des groupes de routage dans l'ordre configuré |

## Recommandations (Guidelines)

- **Groupe de routage (Route Group)** désigne uniquement le routage des modèles et la facturation, pas un niveau de compte ni une permission utilisateur.
- **Prix du modèle (Model Price)** désigne les données du catalogue et de facturation ; il ne s'agit pas d'un prix de recharge, d'abonnement ou de vente publique.
- Conserver **Ratio**, **Token**, **API Token** et **Access Token** comme termes techniques lorsque le contexte l'exige.
